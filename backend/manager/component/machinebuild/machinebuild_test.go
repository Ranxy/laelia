package machinebuild

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "0.2.0", "0.2.0", 0},
		{"older", "0.2.0", "0.3.0", -1},
		{"newer", "0.3.0", "0.2.0", 1},
		{"patch older", "0.2.0", "0.2.1", -1},
		{"major older", "1.0.0", "2.0.0", -1},
		{"leading v", "v0.2.0", "0.2.0", 0},
		{"two parts", "0.2", "0.2.0", 0},
		{"one part", "1", "1.0.0", 0},
		{"dev equal", "dev", "local", 0},
		{"dev vs release", "dev", "0.2.0", 0},
		{"empty", "", "0.2.0", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestUpgradeAvailable(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"older", "0.2.0", "0.3.0", true},
		{"same", "0.3.0", "0.3.0", false},
		{"newer", "0.4.0", "0.3.0", false},
		{"empty latest", "0.2.0", "", false},
		{"empty current", "", "0.3.0", false},
		{"dev current", "dev", "0.3.0", false},
		{"dev latest", "0.2.0", "dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpgradeAvailable(tt.current, tt.latest); got != tt.want {
				t.Errorf("UpgradeAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestLatestPromptBundleVersion(t *testing.T) {
	SetManifest([]byte(`{"version":"1.2.3","prompt_bundle_version":"abc123","targets":{}}`))
	if got := LatestPromptBundleVersion(); got != "abc123" {
		t.Fatalf("LatestPromptBundleVersion() = %q, want abc123", got)
	}
	// A manifest without prompt_bundle_version (older builds) yields "".
	SetManifest([]byte(`{"version":"1.0.0","targets":{}}`))
	if got := LatestPromptBundleVersion(); got != "" {
		t.Fatalf("LatestPromptBundleVersion() = %q, want empty for manifest without field", got)
	}
	// No manifest set at all.
	mu.Lock()
	old := current
	current = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = old
		mu.Unlock()
	})
	if got := LatestPromptBundleVersion(); got != "" {
		t.Fatalf("LatestPromptBundleVersion() = %q, want empty when no manifest", got)
	}
}

// The download route must serve the manifest's gz file name: the embed build
// appends -no-pi to every artifact when pi is not embedded, so the name
// cannot be derived from the target alone.
func TestGzFileName(t *testing.T) {
	SetManifest([]byte(`{"version":"1.0.0","targets":{
		"linux-x64":{"file":"laelia-machine-linux-x64-no-pi","sha256":"aa","gz":{"file":"laelia-machine-linux-x64-no-pi.gz","sha256":"bb"}},
		"windows-x64":{"file":"laelia-machine-windows-x64","sha256":"cc","gz":{"file":"laelia-machine-windows-x64.gz","sha256":"dd"}},
		"darwin-arm64":{"file":"laelia-machine-darwin-arm64","sha256":"ee","gz":{"sha256":"ff"}}
	}}`))
	if got, ok := GzFileName("linux-x64"); !ok || got != "laelia-machine-linux-x64-no-pi.gz" {
		t.Fatalf("GzFileName(linux-x64) = %q,%v; want manifest name", got, ok)
	}
	if got, ok := GzFileName("windows-x64"); !ok || got != "laelia-machine-windows-x64.gz" {
		t.Fatalf("GzFileName(windows-x64) = %q,%v; want manifest name", got, ok)
	}
	// A target whose manifest entry lacks gz.file falls back.
	if _, ok := GzFileName("darwin-arm64"); ok {
		t.Fatal("GzFileName must fail when the manifest has no gz.file")
	}
	if _, ok := GzFileName("unknown"); ok {
		t.Fatal("GzFileName must fail for unknown targets")
	}
}
