package daemon

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Ranxy/laelia/backend/agent/chattools"
)

func TestAuthorize(t *testing.T) {
	s := &Server{sessionToken: "sekret"}

	// Missing header → TOKEN_MISSING.
	r := mustRequest(http.Header{})
	e := s.authorize(r)
	assert.Equal(t, "TOKEN_MISSING", e.Code)

	// Wrong token → TOKEN_INVALID.
	r = mustRequest(http.Header{"Authorization": []string{"Bearer wrong"}})
	e = s.authorize(r)
	assert.Equal(t, "TOKEN_INVALID", e.Code)

	// Correct token → authorized.
	r = mustRequest(http.Header{"Authorization": []string{"Bearer sekret"}})
	assert.Nil(t, s.authorize(r))
}

func TestDepsFallsBackToAgentResourceID(t *testing.T) {
	s := &Server{agentResourceID: "default-agent"}
	d := s.deps(Request{Agent: "", Command: "c"})
	assert.Equal(t, "default-agent", d.Agent)
	assert.Equal(t, "c", d.Command)

	d = s.deps(Request{Agent: "explicit"})
	assert.Equal(t, "explicit", d.Agent)
}

func TestAsChatError(t *testing.T) {
	assert.Nil(t, asChatError(nil))
	e := asChatError(&chattools.Error{Code: "NOT_FOUND_FAILED", Message: "x"})
	assert.Equal(t, "NOT_FOUND_FAILED", e.Code)

	// Non-chattools errors are wrapped as a generic server failure.
	e = asChatError(http.ErrAbortHandler)
	assert.Equal(t, "SERVER_5XX", e.Code)
}

func mustRequest(h http.Header) *http.Request {
	r := &http.Request{Header: h}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	return r
}
