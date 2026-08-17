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
