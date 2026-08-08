package acp2

import (
	"encoding/json"
	"fmt"
)

// Message is one JSON-RPC 2.0 message on the wire. ID, Params, and Result
// stay raw so unknown shapes round-trip untouched.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// IsRequest reports whether the message is a server-to-client request.
func (m *Message) IsRequest() bool { return m.ID != nil && m.Method != "" }

// IsNotification reports whether the message is a server notification.
func (m *Message) IsNotification() bool { return m.ID == nil && m.Method != "" }

// IsResponse reports whether the message is a response to one of our calls.
func (m *Message) IsResponse() bool { return m.ID != nil && m.Method == "" }

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements error.
func (e *RPCError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("acp2: %s (code %d)", e.Message, e.Code)
	}
	return fmt.Sprintf("acp2: rpc error code %d", e.Code)
}

// request is the outbound request frame.
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// notification is the outbound notification frame.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// ClientInfo identifies this client to the agent server.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities advertises the client's protocol capabilities. ExperimentalAPI
// gates the v2 thread protocol.
type Capabilities struct {
	ExperimentalAPI bool `json:"experimentalApi,omitempty"`
}

// InitializeParams is the initialize request payload.
type InitializeParams struct {
	ClientInfo   ClientInfo   `json:"clientInfo"`
	Capabilities Capabilities `json:"capabilities,omitempty"`
}

// InitResult is the initialize response. A compatible server always reports a
// non-empty UserAgent; servers that omit it do not speak the thread protocol.
type InitResult struct {
	UserAgent       string `json:"userAgent"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	Version         string `json:"version,omitempty"`
}

// ThreadStartParams is the thread/start and thread/resume request payload.
// ThreadID is set only for thread/resume.
type ThreadStartParams struct {
	ThreadID              string        `json:"threadId,omitempty"`
	Cwd                   string        `json:"cwd"`
	ApprovalPolicy        string        `json:"approvalPolicy"`
	Sandbox               string        `json:"sandbox"`
	SandboxMode           string        `json:"sandbox_mode,omitempty"`
	DeveloperInstructions string        `json:"developerInstructions,omitempty"`
	Model                 string        `json:"model,omitempty"`
	Config                *ThreadConfig `json:"config,omitempty"`
	ExperimentalRawEvents bool          `json:"experimentalRawEvents,omitempty"`
}

// ThreadConfig carries thread-level agent configuration.
type ThreadConfig struct {
	ModelReasoningEffort string `json:"model_reasoning_effort,omitempty"`
}

// Thread is the thread/start or thread/resume response.
type Thread struct {
	ID string `json:"id"`
}

// TextBlock is one input content block.
type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TurnStartParams is the turn/start request payload.
type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []TextBlock `json:"input"`
}

// SteerParams is the turn/steer request payload. ExpectedTurnID guards
// against racing a turn that already completed.
type SteerParams struct {
	ThreadID       string      `json:"threadId"`
	ExpectedTurnID string      `json:"expectedTurnId"`
	Input          []TextBlock `json:"input"`
}

// Turn is the turn/start or turn/steer response. Some servers report the id
// as result.turnId instead of result.turn.id; the client tolerates both.
type Turn struct {
	ID     string `json:"id"`
	TurnID string `json:"turnId"`
}

// ResolvedID returns the turn id regardless of which response shape the
// server used.
func (t *Turn) ResolvedID() string {
	if t.ID != "" {
		return t.ID
	}
	return t.TurnID
}

// Model describes one selectable model advertised by the server.
type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
}

// ModelListResult is one page of model/list. Servers may return either a
// "data" or a "models" array; NextCursor pages the list.
type ModelListResult struct {
	Data       []Model `json:"data"`
	Models     []Model `json:"models"`
	NextCursor string  `json:"nextCursor"`
}

// UnmarshalJSON tolerates the two thread response shapes: result.thread.id
// and a flat result.threadId/id.
func (t *Thread) UnmarshalJSON(data []byte) error {
	var raw struct {
		Thread *struct {
			ID string `json:"id"`
		} `json:"thread"`
		ThreadID string `json:"threadId"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.Thread != nil:
		t.ID = raw.Thread.ID
	case raw.ThreadID != "":
		t.ID = raw.ThreadID
	default:
		t.ID = raw.ID
	}
	return nil
}

// UnmarshalJSON tolerates the turn response shapes: result.turn.id, a flat
// result.turnId, and a flat result.id.
func (t *Turn) UnmarshalJSON(data []byte) error {
	var raw struct {
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
		TurnID string `json:"turnId"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch {
	case raw.Turn != nil:
		t.ID = raw.Turn.ID
	case raw.TurnID != "":
		t.ID = raw.TurnID
	default:
		t.ID = raw.ID
	}
	return nil
}
