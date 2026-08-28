package webpush

import "testing"

func TestNormalizeSubject(t *testing.T) {
	const def = defaultVAPIDSubject
	tests := []struct {
		name        string
		subject     string
		externalURL string
		want        string
	}{
		{"empty stored and external", "", "", def},
		{"https external wins", "", "https://push.example.com", "https://push.example.com"},
		{"https external origin only", "", "https://push.example.com/base/", "https://push.example.com"},
		{"http external derives mailto", "", "http://push.example.com:8181", "mailto:noreply@push.example.com"},
		{"localhost external rejected", "", "http://localhost:8181", def},
		{"ipv4 external rejected", "", "http://192.168.1.5:8181", def},
		{"legacy default repaired", "mailto:laelia@localhost", "", def},
		{"legacy default healed by external", "mailto:laelia@localhost", "https://chat.example.com", "https://chat.example.com"},
		{"double mailto rejected", "mailto:mailto:admin@example.com", "", def},
		{"stored mailto kept", "mailto:admin@example.com", "https://other.example.com", "mailto:admin@example.com"},
		{"stored https kept", "https://admin.example.com", "https://other.example.com", "https://admin.example.com"},
		{"stored http url invalid", "http://admin.example.com", "", def},
		{"stored localhost invalid", "mailto:admin@localhost", "https://x.example.com", "https://x.example.com"},
		{"stored trimmed", "  https://admin.example.com  ", "", "https://admin.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSubject(tt.subject, tt.externalURL); got != tt.want {
				t.Errorf("NormalizeSubject(%q, %q) = %q, want %q", tt.subject, tt.externalURL, got, tt.want)
			}
		})
	}
}

func TestVapidSubscriber(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"mailto:noreply@laelia.dev", "noreply@laelia.dev"},
		{"https://push.example.com", "https://push.example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := vapidSubscriber(tt.subject); got != tt.want {
			t.Errorf("vapidSubscriber(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

// NewSender must hand webpush-go a subscriber that survives its "prefix
// anything non-https with mailto:" behavior: the legacy mailto:laelia@localhost
// default would otherwise reach Apple as "mailto:mailto:laelia@localhost" and
// fail with 403 BadJwtToken.
func TestNewSenderSubscriber(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{"legacy localhost default", "mailto:laelia@localhost", "noreply@laelia.dev"},
		{"empty", "", "noreply@laelia.dev"},
		{"https passthrough", "https://push.example.com", "https://push.example.com"},
		{"mailto stripped", "mailto:admin@example.com", "admin@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSender("pub", "priv", tt.subject, nil)
			if got := s.sendOptions(nil).Subscriber; got != tt.want {
				t.Errorf("Subscriber = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"push.example.com", true},
		{"push.example.com.", true},
		{"localhost", false},
		{"sub.localhost", false},
		{"device.local", false},
		{"x.invalid", false},
		{"192.168.1.5", false},
		{"::1", false},
		{"nosuffix", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := publicHost(tt.host); got != tt.want {
			t.Errorf("publicHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}
