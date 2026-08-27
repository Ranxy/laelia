//go:build release && no_embed_pi

package pi

import (
	"strings"
	"testing"
)

func TestResolveBinaryNoEmbedPi(t *testing.T) {
	_, err := ResolveBinary()
	if err == nil {
		t.Fatal("expected ResolveBinary to fail when pi is not embedded")
	}
	if !strings.Contains(err.Error(), "does not embed the pi runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}
