package store

import "testing"

func TestMarkdownToPlainText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "emphasis",
			in:   "Some simple paragraph with **bold** and *italic*",
			want: "Some simple paragraph with bold and italic",
		},
		{
			name: "link and image",
			in:   "See [docs](https://example.com) and ![alt](img.png)",
			want: "See docs and alt",
		},
		{
			name: "code span keeps identifier",
			in:   "Use `io.Copy` and `file_name` here",
			want: "Use io.Copy and file_name here",
		},
		{
			name: "list and heading",
			in:   "# Title\n- item one\n1. item two",
			want: "Title item one item two",
		},
		{
			name: "blockquote and rule",
			in:   "> quoted\n---\nafter",
			want: "quoted after",
		},
		{
			name: "fence keeps code",
			in:   "before\n```go\nfunc main() {}\n```\nafter",
			want: "before func main() {} after",
		},
		{
			name: "whitespace collapses",
			in:   "line one\n\nline two",
			want: "line one line two",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownToPlainText(tt.in); got != tt.want {
				t.Errorf("markdownToPlainText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
