package quantity

import (
	"math"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Milli
	}{
		{"2", 2000},
		{"0", 0},
		{"0.5", 500},
		{"1.5", 1500},
		{"500m", 500},
		{"1500m", 1500},
		{"1.5k", 1_500_000},
		{"2M", 2 * 1000 * 1000 * 1000},
		{"1.5G", 1500 * 1000 * 1000 * 1000},
		{"512Mi", 512 * 1000 << 20},
		{"10Gi", 10 * 1000 << 30},
		{"1.5Ki", 1536 * 1000},
		{"1Ti", 1000 << 40},
		{"1Pi", 1000 << 50},
		{"8Pi", 8 * 1000 << 50},
		{"100000", 100000 * 1000},
	}
	for _, tt := range tests {
		if got, err := Parse(tt.in); err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %d, %v; want %d, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestParseEqual(t *testing.T) {
	// "1" and "1000m" are the same quantity; so are "0.5" and "500m".
	pairs := [][2]string{
		{"1", "1000m"},
		{"0.5", "500m"},
		{"1.5Ki", "1536"},
	}
	for _, pair := range pairs {
		a, err := Parse(pair[0])
		if err != nil {
			t.Fatalf("Parse(%q): %v", pair[0], err)
		}
		b, err := Parse(pair[1])
		if err != nil {
			t.Fatalf("Parse(%q): %v", pair[1], err)
		}
		if a != b {
			t.Errorf("Parse(%q) = %d != Parse(%q) = %d", pair[0], a, pair[1], b)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{
		"",                     // empty
		"   ",                  // blank
		"m",                    // no numeric part
		".5",                   // leading dot
		"-2",                   // negative
		"+2",                   // sign
		"1.",                   // trailing dot
		"1.2.3",                // second dot
		"0.5m",                 // below milli resolution
		"0.0001",               // too precise
		"1E",                   // exa decimal overflows
		"1Ei",                  // exa binary overflows
		"2K",                   // uppercase kilo (SI uses lowercase k)
		"2kb",                  // unknown suffix
		"2 GB",                 // space
		"2e3",                  // exponent notation
		"..5",                  // double dot
		"99999999999999999999", // oversized integer part
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %d, nil; want error", in, got)
		}
	}
}

func TestParseOrdering(t *testing.T) {
	// Bounds comparisons the manager validator makes: 500m < 1 < 32Ti.
	small, smallErr := Parse("500m")
	unit, unitErr := Parse("1")
	big, bigErr := Parse("32Ti")
	if smallErr != nil || unitErr != nil || bigErr != nil {
		t.Fatalf("parse errors: %v %v %v", smallErr, unitErr, bigErr)
	}
	if small >= unit || unit >= big {
		t.Errorf("ordering broken: %d, %d, %d", small, unit, big)
	}
}

func TestMilliMax(t *testing.T) {
	if milliMax != Milli(math.MaxInt64) {
		t.Errorf("milliMax = %d, want MaxInt64", milliMax)
	}
}
