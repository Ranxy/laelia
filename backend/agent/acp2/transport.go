// Package acp2 implements a generic client for the ACP v2 thread protocol
// spoken by codex today and, in the future, by other agents. The package is
// provider-agnostic: each provider supplies an EventMapper that translates
// its notification shapes into the neutral events defined here.
package acp2

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

// Transport writes newline-delimited JSON-RPC 2.0 frames. Concurrent writes
// are safe; reads happen on a single goroutine via ReadMessage.
type Transport struct {
	mu  sync.Mutex
	enc *json.Encoder
}

// NewTransport returns a Transport writing NDJSON frames to w.
func NewTransport(w io.Writer) *Transport {
	return &Transport{enc: json.NewEncoder(w)}
}

// Send encodes v as one NDJSON frame.
func (t *Transport) Send(v any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enc.Encode(v)
}

// ReadMessage reads the next non-empty line from r and decodes it as a
// JSON-RPC message. Blank lines are skipped; io.EOF is returned at end of
// stream.
func ReadMessage(r *bufio.Reader) (Message, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return Message{}, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			return Message{}, err
		}
		return m, nil
	}
}
