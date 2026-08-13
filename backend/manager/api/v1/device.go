package v1

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/device"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// deviceSessionTTLSeconds is how long a pending device login stays valid.
	deviceSessionTTLSeconds = 600
	// devicePollIntervalSeconds is the poll interval the CLI is told to use.
	devicePollIntervalSeconds = 5
	// deviceMinPollInterval is the server-side minimum between two polls of
	// the same session, so a misbehaving CLI cannot hammer the anonymous RPC.
	deviceMinPollInterval = 2 * time.Second
	// deviceUserCodeAlphabet is the unambiguous alphabet for user codes
	// (no 0/O, 1/I/L).
	deviceUserCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	deviceUserCodeLength   = 8
)

// DeviceService implements DeviceService: the OAuth2-style device code flow
// that replaces bootstrap-token machine registration. The machine CLI starts
// a session (no credential yet), prints the verification URL + user code, and
// polls; a logged-in user approves on the public /login/device page; on
// approval the manager mints the machine's refresh token (creating the
// machine row on first-time setup).
type DeviceService struct {
	v1connect.UnimplementedDeviceServiceHandler
	deviceStore *device.Store
	store       *store.Store
	secret      string
	profile     *config.Profile
	iam         *iam.Manager
}

func NewDeviceService(ds *device.Store, s *store.Store, secret string, profile *config.Profile, iamManager *iam.Manager) *DeviceService {
	return &DeviceService{
		deviceStore: ds,
		store:       s,
		secret:      secret,
		profile:     profile,
		iam:         iamManager,
	}
}

// StartDeviceLogin begins a device login session and returns the codes the
// CLI displays. The verification path is relative; the CLI composes the full
// URL from its configured manager URL.
func (s *DeviceService) StartDeviceLogin(_ context.Context, req *connect.Request[v1pb.StartDeviceLoginRequest]) (*connect.Response[v1pb.StartDeviceLoginResponse], error) {
	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate device code"))
	}
	userCode := generateUserCode()

	s.deviceStore.Start(&device.Session{
		DeviceCode:  deviceCode,
		UserCode:    userCode,
		MachineID:   req.Msg.MachineId,
		Hostname:    req.Msg.Hostname,
		OS:          req.Msg.Os,
		Arch:        req.Msg.Arch,
		IP:          req.Msg.Ip,
		Version:     req.Msg.Version,
		Fingerprint: req.Msg.Fingerprint,
	})

	return connect.NewResponse(&v1pb.StartDeviceLoginResponse{
		DeviceCode:       deviceCode,
		UserCode:         userCode,
		VerificationPath: fmt.Sprintf("/login/device?user_code=%s", userCode),
		ExpiresIn:        deviceSessionTTLSeconds,
		Interval:         devicePollIntervalSeconds,
	}), nil
}

// PollDeviceLogin returns the session status to the CLI. The device code is
// the bearer secret; an unknown code is reported as EXPIRED so existence is
// not leaked. An APPROVED session keeps returning its result within the
// post-approval grace window so a crashed CLI can recover by re-polling.
func (s *DeviceService) PollDeviceLogin(_ context.Context, req *connect.Request[v1pb.PollDeviceLoginRequest]) (*connect.Response[v1pb.PollDeviceLoginResponse], error) {
	if req.Msg.DeviceCode == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("device_code is required"))
	}
	sess := s.deviceStore.GetByDeviceCode(req.Msg.DeviceCode)
	if sess == nil {
		return connect.NewResponse(&v1pb.PollDeviceLoginResponse{Status: v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_EXPIRED}), nil
	}
	if !s.deviceStore.TouchPoll(sess, deviceMinPollInterval) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("polling too frequently"))
	}

	switch sess.Status {
	case device.StatusPending:
		return connect.NewResponse(&v1pb.PollDeviceLoginResponse{Status: v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_PENDING}), nil
	case device.StatusApproved:
		return connect.NewResponse(&v1pb.PollDeviceLoginResponse{
			Status:       v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_APPROVED,
			MachineId:    sess.Result.MachineID,
			MachineTitle: sess.Result.MachineTitle,
			RefreshToken: sess.Result.RefreshToken,
		}), nil
	case device.StatusDenied:
		return connect.NewResponse(&v1pb.PollDeviceLoginResponse{
			Status:       v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_DENIED,
			DenialReason: sess.DenialReason,
		}), nil
	default:
		return connect.NewResponse(&v1pb.PollDeviceLoginResponse{Status: v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_EXPIRED}), nil
	}
}

// GetDeviceLoginStatus backs the public approval page. Only non-secret device
// info is returned; the user code is the lookup key the device screen shows.
func (s *DeviceService) GetDeviceLoginStatus(ctx context.Context, req *connect.Request[v1pb.GetDeviceLoginStatusRequest]) (*connect.Response[v1pb.GetDeviceLoginStatusResponse], error) {
	if req.Msg.UserCode == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_code is required"))
	}
	sess := s.deviceStore.GetByUserCode(req.Msg.UserCode)
	if sess == nil {
		return connect.NewResponse(&v1pb.GetDeviceLoginStatusResponse{Status: v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_EXPIRED}), nil
	}

	resp := &v1pb.GetDeviceLoginStatusResponse{
		Status:   convertDeviceStatus(sess.Status),
		UserCode: sess.UserCode,
		Hostname: sess.Hostname,
		Os:       sess.OS,
		Arch:     sess.Arch,
		Ip:       sess.IP,
	}
	if sess.MachineID != "" {
		resp.ReauthExisting = true
		if machine, err := s.store.GetMachineByResourceID(ctx, sess.MachineID); err == nil && machine != nil {
			resp.MachineTitle = machine.Name
			resp.MachineOwner = resolveUserHandle(ctx, s.store, machine.CreatedBy)
		}
	}
	if sess.Status == device.StatusDenied {
		resp.DenialReason = sess.DenialReason
	}
	return connect.NewResponse(resp), nil
}

// ApproveDeviceLogin approves a pending device login. A first-time session
// creates the machine (title = hostname, created_by = approver) and mints its
// refresh token. Re-authentication of an existing machine is restricted to
// its creator or a workspace admin; any other approver gets the session marked
// DENIED with a reason and a PermissionDenied error.
func (s *DeviceService) ApproveDeviceLogin(ctx context.Context, req *connect.Request[v1pb.ApproveDeviceLoginRequest]) (*connect.Response[v1pb.ApproveDeviceLoginResponse], error) {
	if req.Msg.UserCode == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_code is required"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in required to approve a device login"))
	}
	sess := s.deviceStore.GetByUserCode(req.Msg.UserCode)
	if sess == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("device login session not found or expired"))
	}
	if sess.Status != device.StatusPending {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("device login session is not pending"))
	}

	// Re-authentication of an existing machine: only its creator or a
	// workspace admin may approve, so a different account cannot silently take
	// over a machine. Any other approver gets an explicit denial with the
	// owner's identity and the recovery path.
	if sess.MachineID != "" {
		machine, err := s.store.GetMachineByResourceID(ctx, sess.MachineID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to look up machine"))
		}
		if machine != nil && !machine.Deleted {
			if !isMachineAdmin(ctx, s.iam, user, machine) {
				ownerHandle := resolveUserHandle(ctx, s.store, machine.CreatedBy)
				reason := fmt.Sprintf(
					"This machine is already registered to %s (machine %q). Ask the owner or a workspace admin to transfer it to you, then run setup again. To wipe local data and create a brand-new machine on this host, run `laelia-machine setup --force`.",
					ownerHandle, machine.Name)
				s.deviceStore.Deny(sess, reason)
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New(reason))
			}
			machineID, machineTitle, refreshToken, err := s.approveReauth(ctx, sess, user.ID, machine)
			if err != nil {
				return nil, err
			}
			s.deviceStore.Approve(sess, user.ID, &device.Result{
				MachineID:    machineID,
				MachineTitle: machineTitle,
				RefreshToken: refreshToken,
			})
			return connect.NewResponse(&v1pb.ApproveDeviceLoginResponse{}), nil
		}
		// The machine was deleted server-side: fall through and create a new
		// one; the CLI updates its state with the new machine id.
	}

	machineID, machineTitle, refreshToken, err := s.approveNewMachine(ctx, sess, user)
	if err != nil {
		return nil, err
	}
	s.deviceStore.Approve(sess, user.ID, &device.Result{
		MachineID:    machineID,
		MachineTitle: machineTitle,
		RefreshToken: refreshToken,
	})
	return connect.NewResponse(&v1pb.ApproveDeviceLoginResponse{}), nil
}

// approveReauth re-authenticates an existing machine: bump token_version,
// revoke every token, and mint a fresh refresh token bound to the session's
// fingerprint. The version bump also invalidates the machine's live access
// tokens, so its current connection dies and the CLI reconnects with the new
// refresh token.
func (s *DeviceService) approveReauth(ctx context.Context, sess *device.Session, approverID int, machine *store.MachineMessage) (string, string, string, error) {
	newVersion := machine.TokenVersion + 1
	now := time.Now()
	refreshToken, err := auth.GenerateMachineTokenWithFamily(machine.Name, machine.ResourceID, newVersion, auth.TokenTypeRefresh, machine.ResourceID, s.profile.Mode, s.secret, machineRefreshTokenDuration)
	if err != nil {
		return "", "", "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate refresh token"))
	}
	updated, err := s.store.RotateMachineTokens(ctx, machine, newVersion, now, &store.MachineTokenMessage{
		MachineID:   machine.ID,
		TokenHash:   hashToken(refreshToken),
		TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
		TokenFamily: machine.ResourceID,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		Fingerprint: sess.Fingerprint,
		ExpiresAt:   now.Add(machineRefreshTokenDuration),
		CreatedBy:   resolveUserHandle(ctx, s.store, approverID),
	})
	if err != nil {
		return "", "", "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to rotate machine tokens"))
	}
	return updated.ResourceID, updated.Name, refreshToken, nil
}

// approveNewMachine creates the machine row (title = hostname, info from the
// session, created_by = approver) and mints its first refresh token
// atomically. The returned machine carries the minted token in RefreshToken.
func (s *DeviceService) approveNewMachine(ctx context.Context, sess *device.Session, user *store.UserMessage) (string, string, string, error) {
	title := sess.Hostname
	if title == "" {
		title = "machine"
	}
	resourceID := uuid.New().String()
	now := time.Now()
	refreshToken, err := auth.GenerateMachineTokenWithFamily(title, resourceID, 1, auth.TokenTypeRefresh, resourceID, s.profile.Mode, s.secret, machineRefreshTokenDuration)
	if err != nil {
		return "", "", "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate refresh token"))
	}
	created, err := s.store.CreateMachineWithToken(ctx, resourceID, &store.MachineMessage{
		Name:         title,
		TokenVersion: 1,
		Info: &storepb.MachineInfo{
			Hostname: sess.Hostname,
			Os:       sess.OS,
			Arch:     sess.Arch,
			Ip:       sess.IP,
			Version:  sess.Version,
		},
		Status:    &storepb.MachineStatus{},
		CreatedBy: user.ID,
	}, &store.MachineTokenMessage{
		TokenHash:   hashToken(refreshToken),
		TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		Fingerprint: sess.Fingerprint,
		ExpiresAt:   now.Add(machineRefreshTokenDuration),
		CreatedBy:   user.Handle,
	})
	if err != nil {
		return "", "", "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create machine"))
	}
	return created.ResourceID, created.Name, refreshToken, nil
}

func convertDeviceStatus(st device.Status) v1pb.DeviceLoginStatus {
	switch st {
	case device.StatusPending:
		return v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_PENDING
	case device.StatusApproved:
		return v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_APPROVED
	case device.StatusDenied:
		return v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_DENIED
	default:
		return v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_EXPIRED
	}
}

func generateDeviceCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateUserCode() string {
	buf := make([]byte, deviceUserCodeLength)
	// crypto/rand failure is not expected; fall back to a zero code rather
	// than failing the whole login flow.
	if _, err := rand.Read(buf); err == nil {
		for i := range buf {
			buf[i] = deviceUserCodeAlphabet[int(buf[i])%len(deviceUserCodeAlphabet)]
		}
	}
	code := string(buf)
	return code[:4] + "-" + code[4:]
}
