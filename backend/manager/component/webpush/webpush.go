// Package webpush delivers Web Push notifications to browsers subscribed via
// NotificationService.CreatePushSubscription. It implements store.WebPushSender,
// so the store's activity-generation path fires notifications fire-and-forget
// for every directed message. The package also exposes the VAPID public key
// (for GetPushConfig) and GenerateKeys (used by the boot-time initializer).
package webpush

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// sendTimeout caps a single push POST. Push services are external and can be
// slow; a per-send timeout keeps one dead endpoint from blocking the rest of a
// user's subscriptions.
const sendTimeout = 15 * time.Second

// maxConcurrentSends bounds the per-user fan-out so a user with many devices
// cannot spawn unbounded goroutines.
const maxConcurrentSends = 8

// Sender dispatches Web Push notifications using VAPID (RFC 8292) and AES128GCM
// payload encryption (RFC 8291) via SherClockHolmes/webpush-go. It implements
// store.WebPushSender. A Sender with empty VAPID keys is disabled: SendToUser is
// a no-op and Enabled() reports false, so a not-yet-configured deployment still
// works. An optional outbound HTTP proxy (for networks that cannot reach
// browser push services directly) is reconciled from the setting table on each
// SendToUser call: when the stored proxy differs from the one the current
// *http.Client was built for, the transport is rebuilt. The setting store caches
// in memory, so this is cheap, and an admin's UpdatePushConfig takes effect on
// the next send without a restart.
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	enabled    bool
	store      *store.Store

	// mu guards appliedProxy and httpClient. It is held only for the
	// compare-and-rebuild in clientForProxy, never during the HTTP send, so a
	// slow proxy build cannot block concurrent sends.
	mu           sync.Mutex
	appliedProxy string       // the proxy httpClient was built for; "" means direct
	httpClient   *http.Client // nil => no proxy, webpush-go uses its own default
}

// GenerateKeys generates a fresh VAPID keypair (base64url, no padding) for Web
// Push. It wraps the underlying webpush-go generator so callers (the boot-time
// setting initializer) depend only on this package.
func GenerateKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}

// defaultVAPIDSubject is used when the stored subject is empty. A valid mailto:
// or https: URL is required by RFC 8292; some push services (notably APNs)
// reject requests without one.
const defaultVAPIDSubject = "mailto:laelia@localhost"

// NewSender builds a Web Push sender from the stored VAPID keypair and subject.
// When either key is empty the returned sender is disabled (Enabled() == false)
// but still usable as a no-op store.WebPushSender. An empty subject falls back
// to defaultVAPIDSubject so a partially-stored config never sends malformed
// auth. The outbound proxy is not configured here; it is reconciled from the
// setting table on each SendToUser so an admin can change it at runtime.
func NewSender(publicKey, privateKey, subject string, st *store.Store) *Sender {
	enabled := publicKey != "" && privateKey != ""
	if subject == "" {
		subject = defaultVAPIDSubject
	}
	return &Sender{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		enabled:    enabled,
		store:      st,
	}
}

// parseProxy parses an outbound proxy URL. An empty proxy returns a nil URL
// (meaning direct connection). Only http:// and https:// schemes are accepted.
func parseProxy(proxy string) (*url.URL, error) {
	if proxy == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil, errors.Wrap(err, "invalid proxy URL")
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, errors.Errorf("proxy URL scheme must be http or https, got %q", proxyURL.Scheme)
	}
	return proxyURL, nil
}

// ValidateProxy returns nil if proxy is empty or a valid http(s) URL, else an
// error. It is used by the UpdatePushConfig handler to reject bad input before
// persisting, so the admin gets immediate feedback rather than a silent
// fallback to a direct connection on the next send.
func ValidateProxy(proxy string) error {
	_, err := parseProxy(proxy)
	return err
}

// buildProxyClient builds an *http.Client whose Transport routes through the
// given proxy. An empty proxy returns a nil client (direct connection). The
// default transport is cloned so sane dial/TLS timeouts are inherited.
func buildProxyClient(proxy string) (*http.Client, error) {
	proxyURL, err := parseProxy(proxy)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return nil, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	return &http.Client{Transport: transport}, nil
}

// resolveClient returns the HTTP client to use for a batch of sends, reconciling
// it with the proxy currently stored in the setting table. The store caches
// settings in memory, so the read is a cheap map lookup + unmarshal per
// SendToUser call. On a store read failure the last-built client is kept, so a
// transient error does not silently drop a working proxy.
func (s *Sender) resolveClient(ctx context.Context) *http.Client {
	cfg, err := s.store.GetWebPushSetting(ctx)
	if err != nil {
		slog.Warn("failed to read web push proxy from store; keeping previous client", "error", err)
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.httpClient
	}
	return s.clientForProxy(cfg.GetHttpProxy())
}

// clientForProxy returns the HTTP client built for the given proxy, rebuilding
// the transport only when the proxy changed since the last call. An invalid
// proxy is logged once per change and falls back to a direct connection (nil),
// and is recorded as applied so the rebuild (and the warning) does not repeat
// on every send until the proxy changes again.
func (s *Sender) clientForProxy(proxy string) *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if proxy == s.appliedProxy {
		return s.httpClient
	}
	client, err := buildProxyClient(proxy)
	if err != nil {
		slog.Warn("web push http_proxy invalid; falling back to direct connection", "proxy", proxy, "error", err)
		s.appliedProxy = proxy
		s.httpClient = nil
		return nil
	}
	s.appliedProxy = proxy
	s.httpClient = client
	return client
}

// Enabled reports whether Web Push is configured (VAPID keys + subject present).
func (s *Sender) Enabled() bool { return s.enabled }

// PublicKey returns the base64url VAPID public key for the browser subscription.
// Empty when disabled.
func (s *Sender) PublicKey() string { return s.publicKey }

// SendToUser delivers the payload to every push subscription registered for the
// user, fanning out with a bounded worker pool. It implements store.WebPushSender
// and is safe to call on a detached goroutine. Best-effort: every failure is
// logged and swallowed; a missed push is not data corruption. Stale endpoints
// (HTTP 404/410) are deleted so they are not retried indefinitely. The outbound
// proxy is reconciled from the setting table once per call so an admin's
// UpdatePushConfig takes effect immediately.
func (s *Sender) SendToUser(ctx context.Context, principalID int, payload []byte) {
	if !s.enabled || len(payload) == 0 {
		return
	}
	subs, err := s.store.ListWebPushSubscriptions(ctx, principalID)
	if err != nil {
		slog.Warn("failed to list web push subscriptions for push",
			"principalID", principalID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	client := s.resolveClient(ctx)
	opts := s.sendOptions(client)

	sem := make(chan struct{}, maxConcurrentSends)
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			s.send(ctx, sub, payload, opts)
		})
	}
	wg.Wait()
}

// sendOptions builds the webpush-go options for one notification, attaching the
// proxy-configured HTTP client when one is set (nil falls back to the library's
// own &http.Client{}).
func (s *Sender) sendOptions(client *http.Client) *webpush.Options {
	opts := &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             24 * 3600,
	}
	if client != nil {
		opts.HTTPClient = client
	}
	return opts
}

// send posts one encrypted notification and handles the push-service response.
// 404/410 means the subscription is gone (user cleared site data, browser
// rotated, etc.); delete it. 413 (payload too large) is dropped — the store
// truncates payloads well under the limit, so this is unexpected. Other non-2xx
// codes are logged; none propagate.
func (s *Sender) send(ctx context.Context, sub *store.WebPushSubscription, payload []byte, opts *webpush.Options) {
	reqCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	resp, err := webpush.SendNotificationWithContext(
		reqCtx,
		payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh}},
		opts,
	)
	if err != nil {
		slog.Warn("web push send failed",
			"principalID", sub.PrincipalID, "endpoint", sub.Endpoint, "error", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return
	case http.StatusGone, http.StatusNotFound:
		if delErr := s.store.DeleteWebPushSubscriptionByEndpoint(ctx, sub.Endpoint); delErr != nil {
			slog.Warn("failed to delete stale web push subscription",
				"endpoint", sub.Endpoint, "status", resp.StatusCode, "error", delErr)
		}
	default:
		slog.Warn("web push send returned non-2xx",
			"principalID", sub.PrincipalID, "endpoint", sub.Endpoint, "status", resp.StatusCode)
	}
}
