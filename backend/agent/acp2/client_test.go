package acp2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeServer replies to client requests and can emit notifications. A nil
// handler records requests without replying (used to test hang/EOF paths).
type fakeServer struct {
	t   *testing.T
	rd  *bufio.Reader
	enc *json.Encoder

	// clientW is the pipe the server writes into; dropping it simulates EOF
	// for the client read loop.
	clientW io.WriteCloser

	mu        sync.Mutex
	requests  []serverRequest
	responses []Message
	notifs    []Notification

	handler func(method string, params json.RawMessage) (any, *RPCError)
}

type serverRequest struct {
	id     json.RawMessage
	method string
	params json.RawMessage
}

// startFakeServer wires a client to an in-process fake server. handler may be
// nil to hang all requests.
func startFakeServer(t *testing.T, mapper EventMapper, handler func(method string, params json.RawMessage) (any, *RPCError)) (*Client, *fakeServer) {
	t.Helper()
	toServerR, toServerW := io.Pipe()
	toClientR, toClientW := io.Pipe()
	fs := &fakeServer{
		t:       t,
		rd:      bufio.NewReader(toServerR),
		enc:     json.NewEncoder(toClientW),
		clientW: toClientW,
		handler: handler,
	}
	client := NewClient(NewTransport(toServerW), toClientR, mapper)
	client.Start()
	go fs.serve()
	t.Cleanup(func() {
		client.Close()
		fs.dropClient()
		_ = toServerW.Close()
	})
	return client, fs
}

func (fs *fakeServer) serve() {
	for {
		m, err := ReadMessage(fs.rd)
		if err != nil {
			return
		}
		fs.mu.Lock()
		switch {
		case m.IsNotification():
			fs.notifs = append(fs.notifs, Notification{Method: m.Method, Params: m.Params})
		case m.IsRequest():
			fs.requests = append(fs.requests, serverRequest{id: m.ID, method: m.Method, params: m.Params})
		default:
			fs.responses = append(fs.responses, m)
		}
		fs.mu.Unlock()
		if !m.IsRequest() || fs.handler == nil {
			continue
		}
		result, rpcErr := fs.handler(m.Method, m.Params)
		if rpcErr != nil {
			_ = fs.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": rpcErr})
		} else {
			_ = fs.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
	}
}

func (fs *fakeServer) requestCount() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return len(fs.requests)
}

// notify emits a notification to the client.
func (fs *fakeServer) notify(method string, params any) {
	_ = fs.enc.Encode(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// sentNotifications returns the notifications the server received.
func (fs *fakeServer) sentNotifications() []Notification {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]Notification, len(fs.notifs))
	copy(out, fs.notifs)
	return out
}

// dropClient closes the server->client pipe so the client read loop sees EOF.
func (fs *fakeServer) dropClient() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.clientW != nil {
		_ = fs.clientW.Close()
		fs.clientW = nil
	}
}

func TestInitialize(t *testing.T) {
	client, fs := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "initialize" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p InitializeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: -32700, Message: err.Error()}
		}
		if p.ClientInfo.Name != "laelia" || p.ClientInfo.Version != "1.0" {
			return nil, &RPCError{Code: -32700, Message: "bad clientInfo"}
		}
		if !p.Capabilities.ExperimentalAPI {
			return nil, &RPCError{Code: -32700, Message: "experimentalApi required"}
		}
		return map[string]any{"userAgent": "codex-test", "version": "0.1.0"}, nil
	})

	res, err := client.Initialize(context.Background(), "laelia", "1.0")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if res.UserAgent != "codex-test" || res.Version != "0.1.0" {
		t.Fatalf("unexpected init result: %+v", res)
	}
	if fs.requestCount() != 1 {
		t.Fatalf("expected 1 request, got %d", fs.requestCount())
	}
}

func TestInitializeRejectsEmptyUserAgent(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(string, json.RawMessage) (any, *RPCError) {
		return map[string]any{}, nil
	})
	if _, err := client.Initialize(context.Background(), "laelia", "1.0"); err == nil {
		t.Fatal("expected handshake error for empty userAgent")
	}
}

func TestInitializedNotification(t *testing.T) {
	client, fs := startFakeServer(t, nil, nil)
	if err := client.Initialized(); err != nil {
		t.Fatalf("initialized: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var notifs []Notification
	for time.Now().Before(deadline) {
		notifs = fs.sentNotifications()
		if len(notifs) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(notifs) != 1 || notifs[0].Method != "initialized" {
		t.Fatalf("unexpected notifications: %+v", notifs)
	}
}

func TestStartThread(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "thread/start" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p ThreadStartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: -32700, Message: err.Error()}
		}
		if p.Cwd != "/workspace" || p.ApprovalPolicy != "never" || p.Sandbox != "danger-full-access" || !p.ExperimentalRawEvents {
			return nil, &RPCError{Code: -32700, Message: "bad thread params"}
		}
		if p.ThreadID != "" {
			return nil, &RPCError{Code: -32700, Message: "threadId must be empty on start"}
		}
		return map[string]any{"thread": map[string]any{"id": "t-1"}}, nil
	})

	th, err := client.StartThread(context.Background(), ThreadStartParams{
		Cwd:                   "/workspace",
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		ExperimentalRawEvents: true,
	})
	if err != nil {
		t.Fatalf("start thread: %v", err)
	}
	if th.ID != "t-1" {
		t.Fatalf("unexpected thread: %+v", th)
	}
}

func TestResumeThread(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "thread/resume" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p ThreadStartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: -32700, Message: err.Error()}
		}
		if p.ThreadID != "t-1" {
			return nil, &RPCError{Code: -32700, Message: "missing threadId"}
		}
		return map[string]any{"thread": map[string]any{"id": "t-1"}}, nil
	})

	th, err := client.ResumeThread(context.Background(), "t-1", ThreadStartParams{Cwd: "/w"})
	if err != nil {
		t.Fatalf("resume thread: %v", err)
	}
	if th.ID != "t-1" {
		t.Fatalf("unexpected thread: %+v", th)
	}
}

func TestStartTurn(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "turn/start" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p TurnStartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: -32700, Message: err.Error()}
		}
		if p.ThreadID != "t-1" || len(p.Input) != 1 || p.Input[0].Type != "text" || p.Input[0].Text != "hello" {
			return nil, &RPCError{Code: -32700, Message: "bad turn params"}
		}
		return map[string]any{"turn": map[string]any{"id": "turn-1"}}, nil
	})

	turn, err := client.StartTurn(context.Background(), "t-1", "hello")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if turn.ResolvedID() != "turn-1" {
		t.Fatalf("unexpected turn: %+v", turn)
	}
}

func TestStartTurnResultTurnID(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(string, json.RawMessage) (any, *RPCError) {
		return map[string]any{"turnId": "turn-2"}, nil
	})
	turn, err := client.StartTurn(context.Background(), "t-1", "hi")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if turn.ResolvedID() != "turn-2" {
		t.Fatalf("unexpected turn: %+v", turn)
	}
}

func TestSteerTurn(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "turn/steer" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p SteerParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: -32700, Message: err.Error()}
		}
		if p.ThreadID != "t-1" || p.ExpectedTurnID != "turn-1" || len(p.Input) != 1 || p.Input[0].Text != "more" {
			return nil, &RPCError{Code: -32700, Message: "bad steer params"}
		}
		return map[string]any{"turn": map[string]any{"id": "turn-1"}}, nil
	})

	turn, err := client.SteerTurn(context.Background(), "t-1", "turn-1", "more")
	if err != nil {
		t.Fatalf("steer turn: %v", err)
	}
	if turn.ResolvedID() != "turn-1" {
		t.Fatalf("unexpected turn: %+v", turn)
	}
}

func TestListModelsPaginated(t *testing.T) {
	page := 0
	client, _ := startFakeServer(t, nil, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "model/list" {
			return nil, &RPCError{Code: -32601, Message: "unexpected method " + method}
		}
		var p map[string]string
		_ = json.Unmarshal(params, &p)
		switch page {
		case 0:
			page++
			if _, ok := p["cursor"]; ok {
				return nil, &RPCError{Code: -32700, Message: "first page must not carry cursor"}
			}
			return map[string]any{"data": []map[string]any{
				{"id": "gpt-5", "displayName": "GPT-5", "isDefault": true},
				{"id": "gpt-5-mini", "hidden": true},
			}, "nextCursor": "c2"}, nil
		default:
			if p["cursor"] != "c2" {
				return nil, &RPCError{Code: -32700, Message: "missing cursor"}
			}
			return map[string]any{"models": []map[string]any{{"id": "o3"}}}, nil
		}
	})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 3 || models[0].ID != "gpt-5" || !models[0].IsDefault || !models[1].Hidden || models[2].ID != "o3" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestRPCError(t *testing.T) {
	client, _ := startFakeServer(t, nil, func(string, json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: -32001, Message: "boom"}
	})
	_, err := client.CallRaw(context.Background(), "thread/start", map[string]any{})
	if err == nil {
		t.Fatal("expected rpc error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32001 || rpcErr.Message != "boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerRequestRejected(t *testing.T) {
	client, fs := startFakeServer(t, nil, nil)
	// Have the server send a request; the client must answer method-not-found.
	go fs.notifyRequest("client/foo", 99)
	deadline := time.Now().Add(2 * time.Second)
	var m Message
	for time.Now().Before(deadline) {
		if m = fs.lastResponse(); m.ID != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !m.IsResponse() || m.Error == nil || m.Error.Code != -32601 {
		t.Fatalf("expected method-not-found response, got %+v", m)
	}
	if client == nil {
		t.Fatal("client must be non-nil")
	}
}

// lastResponse returns the most recent response the server received.
func (fs *fakeServer) lastResponse() Message {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.responses) == 0 {
		return Message{}
	}
	return fs.responses[len(fs.responses)-1]
}

// notifyRequest sends a server-to-client request with the given id.
func (fs *fakeServer) notifyRequest(method string, id int64) {
	_ = fs.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": map[string]any{}})
}

type testMapper struct{}

func (testMapper) MapNotification(n Notification) []Event {
	if n.Method == "item/agentMessage/delta" {
		var p struct {
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(n.Params, &p)
		return []Event{{Type: EventTextDelta, Text: p.Delta}}
	}
	return []Event{{Type: EventRaw, Raw: n.Params}}
}

func TestEventMapping(t *testing.T) {
	client, fs := startFakeServer(t, testMapper{}, nil)
	fs.notify("item/agentMessage/delta", map[string]any{"delta": "hi", "itemId": "i1"})
	select {
	case ev := <-client.Events():
		if ev.Type != EventTextDelta || ev.Text != "hi" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	if client.Notifications() != nil {
		t.Fatal("Notifications must be nil when a mapper is set")
	}
}

func TestRawNotificationsWithoutMapper(t *testing.T) {
	client, fs := startFakeServer(t, nil, nil)
	fs.notify("turn/started", map[string]any{"turn": map[string]any{"id": "t1"}})
	select {
	case n := <-client.Notifications():
		if n.Method != "turn/started" {
			t.Fatalf("unexpected notification: %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification")
	}
	if client.Events() != nil {
		t.Fatal("Events must be nil without a mapper")
	}
}

func TestCallAfterClose(t *testing.T) {
	client, fs := startFakeServer(t, nil, nil)
	client.Close()
	if _, err := client.CallRaw(context.Background(), "model/list", map[string]any{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if fs.requestCount() != 0 {
		t.Fatalf("no request should reach the server, got %d", fs.requestCount())
	}
}

func TestPendingCallFailsOnEOF(t *testing.T) {
	client, fs := startFakeServer(t, nil, nil)
	done := make(chan error, 1)
	go func() {
		_, err := client.CallRaw(context.Background(), "thread/start", map[string]any{})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for fs.requestCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fs.requestCount() == 0 {
		t.Fatal("request never reached the server")
	}
	fs.dropClient()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on EOF")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call did not fail after EOF")
	}
}
