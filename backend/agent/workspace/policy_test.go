package workspace

import "testing"

func TestIsNeverVisibleEntry(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".aws", true},
		{".gnupg", true},
		{".ssh", true},
		{"machine-token-abc", true},
		{".git", false},
		{".env", false},
		{"token.json", false},
		{"node_modules", false},
	}
	for _, tt := range tests {
		if got := isNeverVisibleEntry(tt.name); got != tt.want {
			t.Errorf("isNeverVisibleEntry(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsHiddenPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".env", true},
		{"src/.cache", true},
		{"a/b/c", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isHiddenPath(tt.path); got != tt.want {
			t.Errorf("isHiddenPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsSecretFilePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".ENV", true},
		{".ENV.local", true},
		{".envrc", false},
		{"config.env", false},
		{"EnvVars.json", false},
		{"secrets.json", true},
		{"SECRETS.json", true},
		{"my-secret", true},
		{"A.Secret", true},
		{"secretary.txt", false},
		{"credentials.yml", true},
		{"Credential.json", true},
		{"token", true},
		{"TOKEN.json", true},
		{"tokens.csv", true},
		{"machine-token-abc", true},
		{"machine-TOKEN-ABC", true},
		{"src/.env", true},
		{"README.md", false},
		{"main.go", false},
		{"mytokenizer.go", false},
	}
	for _, tt := range tests {
		if got := isSecretFilePath(tt.path); got != tt.want {
			t.Errorf("isSecretFilePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
