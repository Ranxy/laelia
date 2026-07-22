package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestS3Configured locks in the setup-checklist predicate: S3 counts as
// configured only when BOTH endpoint and bucket are set. A half-filled config
// must still read as unconfigured so the admin onboarding overlay keeps
// prompting until setup is complete. (Stricter than the s3client "both empty"
// sentinel, which would treat a half-filled config as already configured.)
func TestS3Configured(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		bucket   string
		want     bool
	}{
		{"empty", "", "", false},
		{"endpoint only", "https://s3.example.com", "", false},
		{"bucket only", "", "my-bucket", false},
		{"both set", "https://s3.example.com", "my-bucket", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s3Configured(&models.S3ConfigSetting{Endpoint: tt.endpoint, Bucket: tt.bucket})
			assert.Equal(t, tt.want, got)
		})
	}
}
