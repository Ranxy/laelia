package acp2

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestReadMessage(t *testing.T) {
	in := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n\n{\"jsonrpc\":\"2.0\",\"method\":\"turn/started\"}\n"
	r := bufio.NewReader(strings.NewReader(in))

	m1, err := ReadMessage(r)
	if err != nil {
		t.Fatalf("read first message: %v", err)
	}
	if !m1.IsRequest() || m1.Method != "initialize" || string(m1.ID) != "1" {
		t.Fatalf("unexpected first message: %+v", m1)
	}

	m2, err := ReadMessage(r)
	if err != nil {
		t.Fatalf("read second message: %v", err)
	}
	if !m2.IsNotification() || m2.Method != "turn/started" {
		t.Fatalf("unexpected second message: %+v", m2)
	}

	if _, err := ReadMessage(r); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReadMessageInvalidJSON(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("{not json}\n"))
	if _, err := ReadMessage(r); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTransportSend(t *testing.T) {
	var buf bytes.Buffer
	tr := NewTransport(&buf)
	if err := tr.Send(request{JSONRPC: "2.0", ID: 7, Method: "ping", Params: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if got["jsonrpc"] != "2.0" || got["method"] != "ping" || got["id"] != float64(7) {
		t.Fatalf("unexpected frame: %v", got)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatal("frame must be newline terminated")
	}
}
