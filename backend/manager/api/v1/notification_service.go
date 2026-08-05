package v1

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/webpush"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// pushSubscriptionNamePrefix is the resource-name form for one push
// subscription: "users/{uid}/pushSubscriptions/{endpointKey}", where
// endpointKey is the URL-safe base64 of the subscription endpoint. The uid is
// the owning user's principal id.
const pushSubscriptionNamePrefix = "pushSubscriptions/"

// NotificationService implements laelia.v1.NotificationService: per-user Web
// Push subscription management. All RPCs are user-scoped — the caller's own
// principal id is the implicit owner, mirroring ListActivities — and gated by
// the IAM interceptor on laelia.pushConfig.* / laelia.pushSubscriptions.*.
// The handlers additionally reject agent callers and enforce name ownership
// server-side. When Web Push is disabled (no VAPID keys), GetPushConfig reports
// enabled=false and CreatePushSubscription returns FailedPrecondition. The
// optional outbound HTTP proxy is admin-managed (laelia.pushConfig.update).
type NotificationService struct {
	store  *store.Store
	sender *webpush.Sender
	iam    *iam.Manager
}

// NewNotificationService builds the Web Push subscription handler. The sender
// may be disabled (empty VAPID keys); it is still non-nil so GetPushConfig can
// report enabled=false cleanly.
func NewNotificationService(stores *store.Store, sender *webpush.Sender, iamManager *iam.Manager) *NotificationService {
	return &NotificationService{store: stores, sender: sender, iam: iamManager}
}

// GetPushConfig reports whether Web Push is enabled and, when it is, returns the
// VAPID public key the browser needs to subscribe. Any authenticated user may
// read it (the key is public by design). The http_proxy field is populated only
// for callers holding laelia.pushConfig.update (admins); other callers receive
// it empty, so the proxy host is not exposed to ordinary members.
func (s *NotificationService) GetPushConfig(ctx context.Context, _ *connect.Request[v1pb.GetPushConfigRequest]) (*connect.Response[v1pb.GetPushConfigResponse], error) {
	resp := &v1pb.GetPushConfigResponse{
		Enabled:        s.sender.Enabled(),
		VapidPublicKey: s.sender.PublicKey(),
	}
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		if canUpdate, err := s.iam.CheckPermission(ctx, permission.PushConfigUpdate, user, nil, nil); err == nil && canUpdate {
			if cfg, err := s.store.GetWebPushSetting(ctx); err == nil {
				resp.HttpProxy = cfg.GetHttpProxy()
			}
		}
	}
	return connect.NewResponse(resp), nil
}

// UpdatePushConfig sets the optional outbound HTTP proxy used when the manager
// posts notifications to browser push services. Admin-only (enforced by the IAM
// interceptor on laelia.pushConfig.update). An empty http_proxy disables the
// proxy. The change is persisted; the sender reconciles from the setting table
// on its next send, so it takes effect immediately without a restart.
func (s *NotificationService) UpdatePushConfig(ctx context.Context, req *connect.Request[v1pb.UpdatePushConfigRequest]) (*connect.Response[v1pb.UpdatePushConfigResponse], error) {
	proxy := req.Msg.GetHttpProxy()
	if err := webpush.ValidateProxy(proxy); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	cfg, err := s.store.GetWebPushSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read web push setting"))
	}
	cfg.HttpProxy = proxy
	if _, err := s.store.UpsertWebPushSetting(ctx, cfg); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to persist web push proxy"))
	}
	return connect.NewResponse(&v1pb.UpdatePushConfigResponse{HttpProxy: proxy}), nil
}

// ListPushSubscriptions returns every push subscription registered for the
// authenticated user, one per device/browser, ordered by creation time (oldest
// first). The frontend uses it to render whether the current browser is
// subscribed and to reconcile a browser-side subscription that is missing
// server-side.
func (s *NotificationService) ListPushSubscriptions(ctx context.Context, _ *connect.Request[v1pb.ListPushSubscriptionsRequest]) (*connect.Response[v1pb.ListPushSubscriptionsResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("ListPushSubscriptions is for authenticated users"))
	}
	subs, err := s.store.ListWebPushSubscriptions(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list push subscriptions"))
	}
	resp := &v1pb.ListPushSubscriptionsResponse{}
	for _, sub := range subs {
		resp.PushSubscriptions = append(resp.PushSubscriptions, &v1pb.PushSubscription{
			Name:     pushSubscriptionName(user.ID, sub.Endpoint),
			Endpoint: sub.Endpoint,
		})
	}
	return connect.NewResponse(resp), nil
}

// CreatePushSubscription registers (or refreshes) a browser push subscription
// for the authenticated user. Idempotent on (user, endpoint). Returns
// FailedPrecondition when Web Push is disabled, InvalidArgument when required
// fields are missing, and PermissionDenied for agent callers.
func (s *NotificationService) CreatePushSubscription(ctx context.Context, req *connect.Request[v1pb.CreatePushSubscriptionRequest]) (*connect.Response[v1pb.PushSubscription], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("CreatePushSubscription is for authenticated users"))
	}
	if !s.sender.Enabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("web push is not enabled on this server (VAPID keys not configured)"))
	}
	endpoint := req.Msg.GetEndpoint()
	p256dh := req.Msg.GetP256Dh()
	auth := req.Msg.GetAuth()
	if endpoint == "" || p256dh == "" || auth == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("endpoint, p256dh, and auth are required"))
	}

	if err := s.store.UpsertWebPushSubscription(ctx, user.ID, endpoint, p256dh, auth); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to store push subscription"))
	}

	return connect.NewResponse(&v1pb.PushSubscription{
		Name:     pushSubscriptionName(user.ID, endpoint),
		Endpoint: endpoint,
	}), nil
}

// DeletePushSubscription removes a push subscription for the authenticated user.
// The name is "users/{user}/pushSubscriptions/{endpointKey}"; the {user} segment
// is decorative (the server always scopes to the authenticated caller, since the
// caller is the only owner that matters), and only the endpointKey is used to
// identify the subscription. The store scopes by principal_id as the second line
// of defense, so a caller can only delete its own rows.
func (s *NotificationService) DeletePushSubscription(ctx context.Context, req *connect.Request[v1pb.DeletePushSubscriptionRequest]) (*connect.Response[emptypb.Empty], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("DeletePushSubscription is for authenticated users"))
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	_, endpoint, err := parsePushSubscriptionName(name)
	if err != nil {
		return nil, err
	}

	if err := s.store.DeleteWebPushSubscription(ctx, user.ID, endpoint); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete push subscription"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// pushSubscriptionName builds "users/{uid}/pushSubscriptions/{endpointKey}",
// where endpointKey is the URL-safe base64 of the endpoint (no padding), so the
// name is a valid single-segment resource id.
func pushSubscriptionName(uid int, endpoint string) string {
	return fmt.Sprintf("%s%d/%s%s", common.UserNamePrefix, uid, pushSubscriptionNamePrefix, endpointKey(endpoint))
}

// endpointKey is the URL-safe base64 encoding of the endpoint without padding,
// suitable as a path segment in the subscription resource name.
func endpointKey(endpoint string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(endpoint))
}

// parsePushSubscriptionName parses "users/{uid}/pushSubscriptions/{endpointKey}"
// into the owning user id and the decoded endpoint.
func parsePushSubscriptionName(name string) (userID int, endpoint string, err error) {
	tokens, perr := common.GetNameParentTokens(name, common.UserNamePrefix, pushSubscriptionNamePrefix)
	if perr != nil {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, perr)
	}
	uid, perr := strconv.Atoi(tokens[0])
	if perr != nil {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user id in push subscription name %q", name))
	}
	endpointBytes, derr := base64.RawURLEncoding.DecodeString(tokens[1])
	if derr != nil {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(derr, "invalid endpoint key in push subscription name %q", name))
	}
	return uid, string(endpointBytes), nil
}
