package common

import (
	"strings"
	"testing"
)

func TestNormalizeReactionEmoji(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "thumbs up", in: "👍", want: "👍"},
		{name: "check", in: "✅", want: "✅"},
		{name: "trims surrounding whitespace", in: "  👍  ", want: "👍"},
		{name: "skin tone modifier (two runes)", in: "👍🏽", want: "👍🏽"},
		{name: "flag (regional indicators)", in: "🇨🇳", want: "🇨🇳"},
		{name: "family emoji (several runes)", in: "👨‍👩‍👧‍👦", want: "👨‍👩‍👧‍👦"},
		{name: "empty", in: "", wantErr: true},
		{name: "only whitespace", in: "   ", wantErr: true},
		{name: "trailing tab trimmed", in: "👍\t", want: "👍"},
		{name: "trailing newline trimmed", in: "👍\n", want: "👍"},
		{name: "zero-width space rejected", in: "👍\u200b", wantErr: true},
		{name: "trailing em space trimmed", in: "👍\u2003", want: "👍"},
		{name: "text rejected", in: "thumbs up", wantErr: true},
		// A single non-whitespace word passes by design (the guard only rejects
		// whitespace); agent guidance is the real safeguard against text.
		{name: "single-word text passes", in: "okay", want: "okay"},
		{name: "over 16 runes rejected", in: strings.Repeat("👍", 17), wantErr: true},
		{name: "exactly 16 runes ok", in: strings.Repeat("👍", 16), want: strings.Repeat("👍", 16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeReactionEmoji(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeReactionEmoji(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeReactionEmoji(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeReactionEmoji(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
