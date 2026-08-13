package common

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/errors"
)

// maxReactionEmojiRunes caps the length of a normalized reaction emoji. Emoji
// are multi-byte and often multi-codepoint grapheme clusters (e.g. 👍🏽 is two
// runes, a family emoji is several), so the bound is counted in runes, not
// bytes. The real guard against text like "thumbs up" is the whitespace check.
const maxReactionEmojiRunes = 16

// normalizeReactionEmoji validates and canonicalizes a reaction emoji: it must
// be non-empty after trimming, contain no whitespace (space, tab, newline, or
// any Unicode space like U+2000–U+200A), and be at most maxReactionEmojiRunes
// runes. A whitespace-containing value is a single-emoji violation — this is
// what rejects "thumbs up" style text. Note single non-whitespace words (e.g.
// "okay") are NOT rejected: the confirmed design only guards whitespace, and
// the agent guidance is the real guard against text. The returned string is
// the trimmed input. Used by both the manager (authoritative) and the agent
// CLI (fast local failure) so validation lives in exactly one place.
func NormalizeReactionEmoji(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("emoji is required")
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return "", errors.Errorf("emoji must be a single emoji, not text: whitespace found in %q", s)
		}
		// U+200B (zero-width space) is not classified as whitespace by
		// unicode.IsSpace (White_Space ends at U+200A), but is a common
		// obfuscation and would otherwise let text slip through the guard.
		if r == '\u200b' {
			return "", errors.Errorf("emoji must be a single emoji, not text: zero-width space found in %q", s)
		}
	}
	if utf8.RuneCountInString(s) > maxReactionEmojiRunes {
		return "", errors.Errorf("emoji too long (max %d runes)", maxReactionEmojiRunes)
	}
	return s, nil
}
