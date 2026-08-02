package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

// These subcommands are the LLM's interface to Laelia during an autonomous drain
// session. The LLM shells out to `laelia-agent message ...` / `laelia-agent
// command context ...`; identity and the daemon socket location come from env
// vars the daemon injected, so no auth flags are needed. On success the daemon's
// canonical human-readable text goes to stdout (exit 0); on failure a labeled
// Error:/Code:/Next action: block goes to stderr (exit 1).

const daemonHTTPTimeout = 60 * time.Second

// ErrCLIFailed is the sentinel a CLI subcommand returns to signal that it has
// already printed the canonical Error:/Code: block to stderr and the process
// should exit non-zero. main converts it into os.Exit(1) without logging.
var ErrCLIFailed = errors.New("cli subcommand failed (already reported on stderr)")

// identity holds the per-session identity + connection info read from env.
type identity struct {
	socket  string
	token   string
	agent   string
	command string
}

// loadIdentity reads the daemon-injected env vars. A missing socket or token is
// a local bootstrap error (MISSING_*/TOKEN_*): it is reported to stderr and ok
// is false — there is no daemon to talk to.
func loadIdentity() (*identity, bool) {
	id := &identity{
		socket:  os.Getenv(daemonsrv.EnvDaemonSocket),
		token:   os.Getenv(daemonsrv.EnvSessionToken),
		agent:   os.Getenv(daemonsrv.EnvAgent),
		command: os.Getenv(daemonsrv.EnvCommand),
	}
	switch {
	case id.socket == "":
		printError("MISSING_DAEMON", "LAELIA_DAEMON_SOCKET is not set", "Run inside a drain session started by `laelia-agent daemon`.")
		return nil, false
	case id.token == "":
		printError("TOKEN_MISSING", "LAELIA_SESSION_TOKEN is not set", "Run inside a drain session started by `laelia-agent daemon`.")
		return nil, false
	}
	return id, true
}

// newDaemonClient builds an http.Client that dials the unix socket directly,
// ignoring the URL host.
func newDaemonClient(socket string) *http.Client {
	return &http.Client{
		Timeout: daemonHTTPTimeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socket)
			},
		},
	}
}

// call posts req to the given daemon endpoint and renders the canonical output.
// Identity from env is merged into req. The return is false (and the canonical
// error block already printed) on any failure; callers return ErrCLIFailed.
func call(endpoint string, req daemonsrv.Request) bool {
	id, ok := loadIdentity()
	if !ok {
		return false
	}
	req.Agent, req.Command = id.agent, id.command
	if cwd, err := os.Getwd(); err == nil {
		req.Cwd = cwd
	}

	body, err := json.Marshal(req)
	if err != nil {
		printError("INVALID_ARGUMENT_FAILED", "failed to encode request: "+err.Error(), "")
		return false
	}

	// The scheme/host are irrelevant: the transport's DialContext dials the unix
	// socket directly. http:// is required only so net/url parses a request.
	httpReq, err := http.NewRequest(http.MethodPost, "http://laelia-agent"+endpoint, bytes.NewReader(body)) //nolint:revive // unix-socket dial ignores scheme/host
	if err != nil {
		printError("INVALID_ARGUMENT_FAILED", "failed to build request: "+err.Error(), "")
		return false
	}
	httpReq.Header.Set("Authorization", "Bearer "+id.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := newDaemonClient(id.socket).Do(httpReq)
	if err != nil {
		printError("DAEMON_UNAVAILABLE", "cannot reach daemon socket: "+err.Error(), "Ensure `laelia-agent daemon` is running and LAELIA_DAEMON_SOCKET points at its socket.")
		return false
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var out daemonsrv.Response
	if err := json.Unmarshal(respBody, &out); err != nil {
		printError("DAEMON_UNAVAILABLE", "daemon returned non-JSON response: "+string(respBody), "The daemon may have crashed or be mismatched in version.")
		return false
	}
	if out.Code != "" {
		printError(out.Code, out.Message, out.NextAction)
		return false
	}
	_, _ = fmt.Fprint(os.Stdout, out.Text)
	return true
}

// printError writes the canonical failure block to stderr.
func printError(code, message, nextAction string) {
	_, _ = fmt.Fprintf(os.Stderr, "Error: %s\nCode: %s\n", message, code)
	if nextAction != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Next action: %s\n", nextAction)
	}
}

// readContentFlag resolves a --content value: "-" means read the full message
// body from stdin (so the LLM can pipe multi-line text without shell quoting).
func readContentFlag(content string) (string, bool) {
	if content != "-" {
		return content, true
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		printError("INVALID_ARGUMENT_FAILED", "failed to read content from stdin: "+err.Error(), "")
		return "", false
	}
	return string(data), true
}

// requireArgs fails with a canonical error if the arg count is wrong.
//
//nolint:unparam // n is intentionally parameterized for future commands needing other counts
func requireArgs(cmd *cobra.Command, n int, args []string) bool {
	if len(args) != n {
		hint := fmt.Sprintf("Run `%s --help` for usage.", cmd.CommandPath())
		// A dropped `#`-prefixed channel address is the most common cause of a
		// short-arg run: `#` starts a shell comment, so `message read #TEAMS`
		// reaches us with no positional arg at all. Tell the agent to single-quote
		// channel addresses so the shell passes them through literally.
		if n > 0 && len(args) < n {
			hint = fmt.Sprintf(
				"Got fewer arguments than expected. If an argument starts with `#` (a channel address such as #general), `#` begins a SHELL COMMENT and was stripped before the command ran — re-run it SINGLE-QUOTED, e.g. `%s '#general'`. %s",
				cmd.CommandPath(), hint)
		}
		printError("INVALID_ARGUMENT_FAILED",
			fmt.Sprintf("%s expects %d positional argument(s), got %d", cmd.CommandPath(), n, len(args)),
			hint)
		return false
	}
	return true
}
