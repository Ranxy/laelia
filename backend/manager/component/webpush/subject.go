// VAPID subject (RFC 8292 "sub" claim) validation and normalization. Push
// services disagree on how strictly they validate it: Apple's web push service
// (web.push.apple.com) returns 403 {"reason":"BadJwtToken"} unless sub is an
// https: URL or a mailto: address at a public domain — localhost, .local,
// .localhost, dotless names, and IP literals are all rejected — while Chrome
// (FCM) and Firefox accept anything. Because the failure is silent (only a
// status code in the manager log), the sender never stores a value Apple
// rejects in the first place.
package webpush

import (
	"net/url"
	"strings"
	"unicode"
)

// defaultVAPIDSubject is used when the stored subject is missing or unusable.
// It must be a well-formed address on a public domain: Apple rejects sub
// values at localhost, .local, .localhost, and IP literals with 403
// BadJwtToken, which silently breaks every iOS/Safari push.
const defaultVAPIDSubject = "mailto:noreply@laelia.dev"

// NormalizeSubject returns a valid VAPID subject for the stored setting and
// the workspace ExternalURL. A stored subject that is already valid wins so an
// admin-set value is preserved; otherwise the ExternalURL is used (as an https:
// URL, or as a mailto: derived from its host); otherwise the default.
// Invalidating a previously-stored subject only affects future push requests —
// the sub claim rides on each VAPID JWT, not on the browser subscription — so
// existing subscriptions keep working after the repair.
func NormalizeSubject(subject, externalURL string) string {
	if isValidVAPIDSubject(strings.TrimSpace(subject)) {
		return strings.TrimSpace(subject)
	}
	if u, err := url.Parse(strings.TrimSpace(externalURL)); err == nil &&
		(u.Scheme == "https" || u.Scheme == "http") && publicHost(u.Hostname()) {
		if u.Scheme == "https" {
			return u.Scheme + "://" + u.Host
		}
		return "mailto:noreply@" + u.Hostname()
	}
	return defaultVAPIDSubject
}

// vapidSubscriber converts a stored subject into the subscriber value passed
// to webpush-go, which prefixes any subscriber not starting with "https:" with
// "mailto:" (v1.4.0 getVAPIDAuthorizationHeader). A stored mailto: subject must
// therefore be stripped first, or the JWT carries the invalid
// "mailto:mailto:..." and Apple rejects it with 403.
func vapidSubscriber(subject string) string {
	if addr, ok := strings.CutPrefix(subject, "mailto:"); ok && addr != "" {
		return addr
	}
	return subject
}

// isValidVAPIDSubject reports whether s is a subject push services accept: an
// https: URL or a mailto: address at a public-domain host.
func isValidVAPIDSubject(subject string) bool {
	if u, err := url.Parse(subject); err == nil && u.Scheme == "https" {
		return publicHost(u.Hostname())
	}
	if addr, ok := strings.CutPrefix(subject, "mailto:"); ok {
		user, domain, found := strings.Cut(addr, "@")
		// Reject leftovers from a doubled prefix ("mailto:mailto:...") and
		// multi-@ forms: the user part must be a bare email local part.
		return found && user != "" && !strings.ContainsAny(user, ":@") &&
			!strings.Contains(domain, "@") && publicHost(domain)
	}
	return false
}

// publicHost reports whether host is a public DNS name rather than a
// loopback/reserved name or an IP literal — the forms Apple's web push service
// rejects in the VAPID sub claim.
func publicHost(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if host == "" || strings.Contains(host, ":") { // bare IPv6
		return false
	}
	if host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".invalid") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false // dotless names and IPv4 literals
	}
	// An IPv4 literal ends in a numeric label; a DNS name ends in a TLD.
	last := labels[len(labels)-1]
	if isAllDigits(last) {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
