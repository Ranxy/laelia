package v1

import "testing"

func TestSingleLinePreview(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "empty", in: "", max: 120, want: ""},
		{name: "whitespace only", in: "  \n\t ", max: 120, want: ""},
		{name: "under limit", in: "hello", max: 120, want: "hello"},
		{name: "exactly at limit", in: "12345", max: 5, want: "12345"},
		{name: "newlines fold", in: "line one\nline two", max: 120, want: "line one line two"},
		{name: "over limit ellipsis", in: "abcdef", max: 3, want: "abc…"},
		{name: "multi-byte rune boundary", in: "你好世界", max: 3, want: "你好世…"},
		{name: "trims surrounding spaces", in: "  padded  ", max: 120, want: "padded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := singleLinePreview(tt.in, tt.max); got != tt.want {
				t.Fatalf("singleLinePreview(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestTruncateContentUsesTaskTitleCap(t *testing.T) {
	if got := truncateContent("x"); got != "x" {
		t.Fatalf("truncateContent should pass through short content, got %q", got)
	}
	if len([]rune(truncateContent(string(make([]rune, maxTaskTitleLen+1))))) != maxTaskTitleLen+1 {
		t.Fatal("truncateContent must cap at maxTaskTitleLen runes plus the ellipsis")
	}
}
