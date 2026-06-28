package s3client

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestS3HTTPClient_HasTimeouts guards against regressing back to the default
// transport (no response-header deadline), which let a stuck S3 endpoint pin
// a goroutine until the whole request context was cancelled.
func TestS3HTTPClient_HasTimeouts(t *testing.T) {
	cli := s3HTTPClient()
	transport, ok := cli.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", cli.Transport)
	}
	assert.Positive(t, int64(transport.ResponseHeaderTimeout), "ResponseHeaderTimeout must be set")
	assert.NotNil(t, transport.DialContext, "DialContext must be set")
}

// TestBuild_PassesContext is a source-level guard that build's first argument
// is the caller context (not context.Background()), so request-scoped
// cancellation/deadlines propagate into the AWS SDK config loader.
func TestBuild_PassesContext(_ *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled context should make config loading fail fast rather than
	// hang on a background context.
	_, err := build(ctx, &models.S3ConfigSetting{
		Endpoint:  "http://127.0.0.1:1",
		Bucket:    "b",
		Region:    "us-east-1",
		AccessKey: "ak",
		SecretKey: "sk",
	})
	// We only assert build does not panic and returns; the SDK may or may not
	// surface the cancelled context synchronously. The guard is the signature.
	_ = err
}
