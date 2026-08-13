//nolint:revive
package common

import (
	"fmt"
	"strings"
	"unicode"
)

// Handle kinds: every user/agent handle embeds one of these as a per-type
// counter suffix ("ran-user-1", "rei-agent-1"). The suffix makes the handle
// self-describing: "@ran-user-1" can only name a user and "@rei-agent-1" only
// an agent, so mention/DM resolution never needs a display-name disambiguation
// step.
const (
	HandleKindUser  = "user"
	HandleKindAgent = "agent"

	// SystemBotHandle is the reserved, fixed handle of the internal SYSTEM_BOT
	// principal (principal.id = 1). No user or agent can ever be assigned it.
	SystemBotHandle = "system-bot"
)

// SlugifyHandle converts a display name into the base segment of a handle:
// lowercased (CJK and other letters preserved), whitespace runs collapsed to
// '-', and every character outside letters/digits/'-'/'_' dropped (so a name
// can never smuggle '@', '/', or spaces into a mention token). Returns ""
// when nothing remains.
func SlugifyHandle(name string) string {
	var b strings.Builder
	prevDash := true // treat start as "already a dash" so leading dashes are skipped
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			_, _ = b.WriteRune(r)
			prevDash = false
		case unicode.IsSpace(r):
			if !prevDash {
				_ = b.WriteByte('-')
				prevDash = true
			}
		case r == '-':
			if !prevDash {
				_ = b.WriteByte('-')
				prevDash = true
			}
		default:
			// drop the rune
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// FormatHandle builds the handle "slug-kind-seq", e.g. FormatHandle("ran",
// HandleKindUser, 2) == "ran-user-2".
func FormatHandle(slug, kind string, seq int) string {
	return fmt.Sprintf("%s-%s-%d", slug, kind, seq)
}

// HandleKindOf reports whether handle carries the per-type suffix of kind
// ("-user-<n>" / "-agent-<n>"). Handles are unique per type, so the suffix
// alone is a reliable type discriminator for "@<handle>" mention/DM
// resolution. The check looks at the LAST "-<kind>-" occurrence and requires
// the remainder to be a positive integer, so a slug that itself contains
// "-user-" or "-agent-" (e.g. "my-agent-1-user-2") cannot confuse the kind.
func HandleKindOf(handle, kind string) bool {
	suffix := "-" + kind + "-"
	i := strings.LastIndex(handle, suffix)
	if i < 0 {
		return false
	}
	rest := handle[i+len(suffix):]
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
