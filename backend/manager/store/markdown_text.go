package store

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	mdCodeSpanRE   = regexp.MustCompile("`([^`]*)`")
	mdImageRE      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLinkRE       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdHTMLTagRE    = regexp.MustCompile(`<[^>]+>`)
	mdEscapeRE     = regexp.MustCompile(`\\([\\` + "`" + `*_{}\[\]()#+\-.!])`)
	mdHeadingRE    = regexp.MustCompile(`^#{1,6}\s+`)
	mdBlockquoteRE = regexp.MustCompile(`^>+\s?`)
	mdListRE       = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)
)

// markdownToPlainText converts chat message markdown into the plain text used
// for search. It is intentionally small and dependency-free: chat content uses
// a limited markdown subset (emphasis, code, links, lists, quotes, headings),
// and the goal is a searchable rendering, not a perfect CommonMark renderer.
// Mentions are plain "@handle" text in content and pass through unchanged.
func markdownToPlainText(content string) string {
	var lines []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			lines = append(lines, line)
			continue
		}
		line = stripMarkdownBlockMarkers(trimmed)
		line = stripMarkdownInline(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func stripMarkdownBlockMarkers(line string) string {
	if line == "" || line == "---" || line == "***" || line == "___" {
		return ""
	}
	line = mdHeadingRE.ReplaceAllString(line, "")
	line = mdBlockquoteRE.ReplaceAllString(line, "")
	line = mdListRE.ReplaceAllString(line, "")
	return line
}

func stripMarkdownInline(line string) string {
	// Code spans keep their inner text verbatim so markdown inside code is not
	// interpreted (and underscores in identifiers survive). They are parked in
	// placeholders while the other inline rules run, then restored.
	codeSpans := mdCodeSpanRE.FindAllStringSubmatch(line, -1)
	for i, m := range codeSpans {
		line = strings.Replace(line, m[0], fmt.Sprintf("\x00%d\x00", i), 1)
	}
	// Links and images collapse to their label/alt text.
	line = mdImageRE.ReplaceAllString(line, "$1")
	line = mdLinkRE.ReplaceAllString(line, "$1")
	// HTML tags are dropped; the space keeps words on either side from gluing.
	line = mdHTMLTagRE.ReplaceAllString(line, " ")
	// Emphasis markers are removed so "**bold**" and "*italic*" search as their
	// rendered text.
	line = strings.ReplaceAll(line, "**", "")
	line = strings.ReplaceAll(line, "__", "")
	line = strings.ReplaceAll(line, "*", "")
	line = strings.ReplaceAll(line, "_", "")
	// Backslash escapes resolve to the escaped character.
	line = mdEscapeRE.ReplaceAllString(line, "$1")
	for i, m := range codeSpans {
		line = strings.ReplaceAll(line, fmt.Sprintf("\x00%d\x00", i), m[1])
	}
	return line
}
