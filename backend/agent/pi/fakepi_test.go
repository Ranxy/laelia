package pi

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// fakePiModeFile is read by the fake-pi subprocess (CWD = the session working
// dir) to decide how to answer a prompt. "settle" (default) accepts the prompt
// and immediately emits agent_settled. "wait" accepts the prompt, emits a text
// delta, then blocks until the next stdin line (an abort) before settling — this
// keeps a turn in flight so lifecycle tests can cancel it mid-turn. "die"
// accepts the prompt, emits a text delta, then exits so the subprocess dies
// mid-turn (exercises the waitPump close-active-channel fast-failure path).
// "stuck" reads stdin but never answers get_state, simulating a pi that spawned
// but is wedged at startup (no response to the first RPC) — exercises the
// Phase 5 startup-timeout fast-failure + wedged-process kill path.
// "steer" accepts the prompt, emits a text delta, then blocks until a `steer`
// command arrives: it responds success, emits another text delta, and settles —
// proving a same-turn steer is delivered while the turn is in flight and the
// turn extends until the steered work is processed. "steer-fail" is the same
// but responds success:false to the steer (the turn must still settle).
// "compact" accepts the prompt, emits compaction_start, waits briefly for a
// steer (which the executor must suppress while compacting), then emits
// compaction_end + agent_settled.
const fakePiModeFile = "fake-pi-mode"

// fakePiPromptsFile receives one JSON line per prompt command the fake pi
// accepts, recording the prompt message. Lifecycle tests read it back to
// assert which prompt text a turn sent — e.g. the cold init prompt vs. a
// warm-turn-only batch (the Phase 6 amnesia regression).
const fakePiPromptsFile = "fake-pi-prompts.log"

// fakePiSteersFile receives one JSON line per steer command the fake pi
// accepts, recording the steer message. Lifecycle tests read it back to assert
// whether (and what) a same-turn steer reached the subprocess.
const fakePiSteersFile = "fake-pi-steers.log"

// This file's init() turns the test binary into a fake pi subprocess when it is
// re-exec'd by a lifecycle test. The runner spawns pi as `testbin --mode rpc
// --provider ...` (the pi launchArgs); a real `go test` run never passes a bare
// --mode, so isFakePiChild distinguishes the child from the parent. init() runs
// before the testing framework parses flags, so the unknown --mode never
// reaches flag.Parse and the child never tries to run the test suite.
func init() {
	if !isFakePiChild() {
		return
	}
	fakePiMain()
	os.Exit(0)
}

func isFakePiChild() bool {
	for _, a := range os.Args[1:] {
		if a == "--mode" || a == "-mode" {
			return true
		}
	}
	return false
}

// fakePiMain speaks just enough of the pi RPC JSONL protocol for the lifecycle
// tests: it routes responses by command id and streams a canned agent_settled
// per prompt. It does not exercise the LLM; it only proves the Go-side session
// keeps the subprocess alive across turns and survives a Cancel.
func fakePiMain() {
	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\n")
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		var head struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
		}
		if json.Unmarshal([]byte(line), &head) != nil {
			continue
		}
		switch head.Type {
		case "get_state":
			if readFakePiMode() == "stuck" {
				// Wedged startup: keep reading stdin but never answer get_state,
				// so resumeOrCapture's send blocks until the startup timeout.
				continue
			}
			writeJSONL(w, response{
				Type:    "response",
				ID:      head.ID,
				Command: "get_state",
				Success: true,
				Data:    json.RawMessage(`{"sessionFile":"/tmp/fake-pi-session.jsonl","sessionId":"fake"}`),
			})
		case "switch_session":
			writeJSONL(w, response{Type: "response", ID: head.ID, Command: "switch_session", Success: true})
		case "get_session_stats":
			writeJSONL(w, response{
				Type:    "response",
				ID:      head.ID,
				Command: "get_session_stats",
				Success: true,
				Data:    json.RawMessage(`{"contextUsage":{"tokens":1000,"contextWindow":200000,"percent":0.5}}`),
			})
		case "prompt":
			var pc struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal([]byte(line), &pc)
			appendFakePiPrompt(pc.Message)
			writeJSONL(w, response{Type: "response", ID: head.ID, Command: "prompt", Success: true})
			writeJSONL(w, event{Type: eventMessageUpdate, AssistantMessageEvent: &assistantMessageEvent{
				Type:         assistantEventTextDelta,
				ContentIndex: 0,
				Delta:        "ok",
			}})
			switch readFakePiMode() {
			case "die":
				// Exit immediately so the subprocess dies mid-turn; waitPump
				// closes the active turn channel and the drain loop fails fast.
				return
			case "wait":
				// Block for the next line (an abort from Cancel) so the turn
				// stays in flight, then settle.
				if _, err := r.ReadString('\n'); err != nil {
					return
				}
			case "steer", "steer-fail":
				// Block until a steer command arrives (the turn stays in
				// flight), respond per mode, then settle. Any other line
				// (e.g. an abort) is ignored.
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					var sc struct {
						Type    string `json:"type"`
						ID      string `json:"id,omitempty"`
						Message string `json:"message"`
					}
					if json.Unmarshal([]byte(line), &sc) != nil || sc.Type != "steer" {
						continue
					}
					appendFakePiSteer(sc.Message)
					success := readFakePiMode() != "steer-fail"
					writeJSONL(w, response{Type: "response", ID: sc.ID, Command: "steer", Success: success, Error: map[bool]string{false: "steer rejected"}[success]})
					writeJSONL(w, event{Type: eventMessageUpdate, AssistantMessageEvent: &assistantMessageEvent{
						Type:         assistantEventTextDelta,
						ContentIndex: 0,
						Delta:        "steered",
					}})
					writeJSONL(w, event{Type: eventAgentSettled})
					return
				}
			case "compact":
				// Emit compaction_start, then wait briefly for a steer (the
				// executor must suppress it while compacting), then finish the
				// compaction and settle either way. The wait uses a goroutine
				// reader (pipes do not support read deadlines); the main loop
				// does not read again before returning, so the bufio.Reader is
				// never accessed concurrently.
				writeJSONL(w, event{Type: eventCompactionStart})
				steerLine := make(chan string, 1)
				go func() {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					steerLine <- line
				}()
				select {
				case line := <-steerLine:
					// A steer arrived while compacting (bug: should have been
					// suppressed). Log it so the test can fail loudly.
					appendFakePiSteer(line)
				case <-time.After(time.Second):
				}
				writeJSONL(w, event{Type: eventCompactionEnd})
				writeJSONL(w, event{Type: eventAgentSettled})
				return
			default:
				// "settle": fall through to the agent_settled write below.
			}
			writeJSONL(w, event{Type: eventAgentSettled})
			switch readFakePiMode() {
			case "die":
				// Exit immediately so the subprocess dies mid-turn; waitPump
				// closes the active turn channel and the drain loop fails fast.
				return
			case "wait":
				// Block for the next line (an abort from Cancel) so the turn
				// stays in flight, then settle.
				if _, err := r.ReadString('\n'); err != nil {
					return
				}
			default:
				// "settle": fall through to the agent_settled write below.
			}
			writeJSONL(w, event{Type: eventAgentSettled})
		case "abort":
			// Abort with no in-flight prompt (e.g. settle-mode turn cancelled
			// before the prompt was sent): settle for symmetry.
			writeJSONL(w, event{Type: eventAgentSettled})
		default:
			// Unknown command: ignore.
		}
	}
}

func readFakePiMode() string {
	b, err := os.ReadFile(fakePiModeFile)
	if err != nil {
		return "settle"
	}
	switch strings.TrimSpace(string(b)) {
	case "wait", "die", "stuck", "steer", "steer-fail", "compact":
		return strings.TrimSpace(string(b))
	default:
		return "settle"
	}
}

func writeJSONL(w *bufio.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

// appendFakePiPrompt appends one prompt message (newline-delimited JSON) to the
// prompts log in the fake pi's CWD (the session working dir), so a lifecycle
// test can inspect which prompt each turn sent.
func appendFakePiPrompt(msg string) {
	f, err := os.OpenFile(fakePiPromptsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(struct {
		Message string `json:"message"`
	}{Message: msg})
}

// appendFakePiSteer appends one steer message to the steers log in the fake
// pi's CWD, so a lifecycle test can assert whether a same-turn steer reached
// the subprocess (and with what text).
func appendFakePiSteer(msg string) {
	f, err := os.OpenFile(fakePiSteersFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(struct {
		Message string `json:"message"`
	}{Message: msg})
}
