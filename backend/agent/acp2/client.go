package acp2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	pkgerrors "github.com/pkg/errors"
)

// eventBufferSize bounds in-flight mapped events between the reader loop and
// the consumer.
const eventBufferSize = 256

// maxModelPages bounds the model/list cursor walk.
const maxModelPages = 5

// ErrClosed is returned by calls issued after the client was closed.
var ErrClosed = errors.New("acp2: client closed")

// Client is a JSON-RPC 2.0 client for the ACP v2 thread protocol over stdio.
// It owns the read loop, routes responses to pending calls, and funnels
// inbound notifications through the optional EventMapper. A nil mapper
// delivers raw notifications on Notifications().
type Client struct {
	tr     *Transport
	reader *bufio.Reader
	mapper EventMapper

	mu        sync.Mutex
	nextID    int64
	pending   map[string]chan Response
	closed    bool
	closeOnce sync.Once

	events        chan Event
	notifications chan Notification
	done          chan struct{}
}

// NewClient builds a client over the given transport and reader. When mapper
// is non-nil, notifications are mapped to events; otherwise they are
// delivered raw. Call Start before issuing requests.
func NewClient(tr *Transport, r io.Reader, mapper EventMapper) *Client {
	c := &Client{
		tr:      tr,
		reader:  bufio.NewReader(r),
		mapper:  mapper,
		pending: map[string]chan Response{},
		done:    make(chan struct{}),
	}
	if mapper != nil {
		c.events = make(chan Event, eventBufferSize)
	} else {
		c.notifications = make(chan Notification, eventBufferSize)
	}
	return c
}

// Start launches the read loop.
func (c *Client) Start() {
	go c.readLoop()
}

// Close marks the client closed. Pending calls fail with ErrClosed; the read
// loop exits when the underlying reader reaches EOF.
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeOnce.Do(func() { close(c.done) })
}

// Events returns mapped events; non-nil only when the client was constructed
// with an EventMapper.
func (c *Client) Events() <-chan Event { return c.events }

// Notifications returns raw notifications; non-nil only when no EventMapper
// was supplied.
func (c *Client) Notifications() <-chan Notification { return c.notifications }

// Call performs a request and decodes the result into result.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	raw, err := c.CallRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(raw, result)
	}
	return nil
}

// CallRaw performs a request and returns the raw result.
func (c *Client) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan Response, 1)
	c.pending[fmt.Sprint(id)] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, fmt.Sprint(id))
		c.mu.Unlock()
	}()
	if err := c.tr.Send(request{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, pkgerrors.Wrapf(err, "acp2: send %s", method)
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrClosed
	}
}

// Initialize performs the protocol handshake. The response must carry a
// non-empty userAgent; a server that omits it does not speak the thread
// protocol.
func (c *Client) Initialize(ctx context.Context, name, version string) (*InitResult, error) {
	var res InitResult
	if err := c.Call(ctx, "initialize", InitializeParams{
		ClientInfo:   ClientInfo{Name: name, Version: version},
		Capabilities: Capabilities{ExperimentalAPI: true},
	}, &res); err != nil {
		return nil, err
	}
	if res.UserAgent == "" {
		return nil, errors.New("acp2: initialize response missing userAgent handshake field")
	}
	return &res, nil
}

// Initialized notifies the server that the client handshake completed.
func (c *Client) Initialized() error {
	return c.tr.Send(notification{JSONRPC: "2.0", Method: "initialized"})
}

// StartThread creates a new thread.
func (c *Client) StartThread(ctx context.Context, p ThreadStartParams) (*Thread, error) {
	var res Thread
	if err := c.Call(ctx, "thread/start", p, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ResumeThread resumes an existing thread by id.
func (c *Client) ResumeThread(ctx context.Context, threadID string, p ThreadStartParams) (*Thread, error) {
	p.ThreadID = threadID
	var res Thread
	if err := c.Call(ctx, "thread/resume", p, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// StartTurn starts a new turn on a thread with the given text input.
func (c *Client) StartTurn(ctx context.Context, threadID, input string) (*Turn, error) {
	var res Turn
	if err := c.Call(ctx, "turn/start", TurnStartParams{
		ThreadID: threadID,
		Input:    []TextBlock{{Type: "text", Text: input}},
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SteerTurn injects a message into the in-flight turn. expectedTurnID
// protects against racing a turn that already completed.
func (c *Client) SteerTurn(ctx context.Context, threadID, expectedTurnID, input string) (*Turn, error) {
	var res Turn
	if err := c.Call(ctx, "turn/steer", SteerParams{
		ThreadID:       threadID,
		ExpectedTurnID: expectedTurnID,
		Input:          []TextBlock{{Type: "text", Text: input}},
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListModels walks the model/list cursor pages and flattens them. Servers
// that report models as "models" instead of "data" are tolerated.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	var models []Model
	cursor := ""
	for page := 0; page < maxModelPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var pageRes ModelListResult
		if err := c.Call(ctx, "model/list", params, &pageRes); err != nil {
			return nil, err
		}
		entries := pageRes.Data
		if len(entries) == 0 {
			entries = pageRes.Models
		}
		models = append(models, entries...)
		if pageRes.NextCursor == "" {
			return models, nil
		}
		cursor = pageRes.NextCursor
	}
	return models, nil
}

// readLoop drains the reader until EOF, routing responses to pending calls,
// notifications to the mapper, and replying to unexpected server requests
// with method-not-found.
func (c *Client) readLoop() {
	defer c.closeOnce.Do(func() { close(c.done) })
	for {
		msg, err := ReadMessage(c.reader)
		if err != nil {
			c.failPending(err)
			return
		}
		switch {
		case msg.IsRequest():
			c.replyUnsupported(msg)
		case msg.IsResponse():
			c.dispatchResponse(msg)
		default:
			c.dispatchNotification(msg)
		}
	}
}

func (c *Client) dispatchResponse(msg Message) {
	c.mu.Lock()
	ch, ok := c.pending[string(msg.ID)]
	c.mu.Unlock()
	if !ok {
		return
	}
	resp := Response{}
	if msg.Error != nil {
		resp.Error = msg.Error
	} else {
		resp.Result = msg.Result
	}
	ch <- resp
}

func (c *Client) dispatchNotification(msg Message) {
	n := Notification{Method: msg.Method, Params: msg.Params}
	if c.mapper == nil {
		select {
		case c.notifications <- n:
		case <-c.done:
		}
		return
	}
	for _, ev := range c.mapper.MapNotification(n) {
		select {
		case c.events <- ev:
		case <-c.done:
			return
		}
	}
}

func (c *Client) replyUnsupported(msg Message) {
	_ = c.tr.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      msg.ID,
		"error": map[string]any{
			"code":    -32601,
			"message": fmt.Sprintf("method %q not supported", msg.Method),
		},
	})
}

// failPending fails every outstanding call with err after the read loop
// terminates.
func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- Response{Error: &RPCError{Code: -32000, Message: err.Error()}}
		delete(c.pending, id)
	}
}

// Response is a decoded JSON-RPC response.
type Response struct {
	Result json.RawMessage
	Error  *RPCError
}
