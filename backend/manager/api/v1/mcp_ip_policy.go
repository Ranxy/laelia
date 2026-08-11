package v1

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/component/mcp"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// mcpTargetResolver resolves hostnames to addresses; *net.Resolver satisfies
// it. It is an interface so tests can inject a fake.
type mcpTargetResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// mcpSettingReader returns the workspace personal-MCP setting; *store.Store
// satisfies it. It is an interface so tests can inject a fake without a
// database.
type mcpSettingReader interface {
	GetUserMcpConfigSetting(ctx context.Context) (*models.UserMcpConfigSetting, error)
}

// defaultMcpTargetResolver is the resolver used in production paths.
var defaultMcpTargetResolver mcpTargetResolver = net.DefaultResolver

// mcpTargetLookupTimeout bounds DNS lookups during save-path validation so a
// slow resolver cannot stall server create/update.
const mcpTargetLookupTimeout = 5 * time.Second

// validateMcpServerTarget checks the server URL against the workspace MCP IP
// policy before persisting. isPersonal mirrors the storage-layer owner
// semantics (owner_id != 0): a personal server carries the caller's user id.
func validateMcpServerTarget(ctx context.Context, settings mcpSettingReader, serverURL string, isPersonal bool) error {
	return validateMcpServerTargetWithResolver(ctx, settings, serverURL, isPersonal, defaultMcpTargetResolver)
}

func validateMcpServerTargetWithResolver(ctx context.Context, settings mcpSettingReader, serverURL string, isPersonal bool, resolver mcpTargetResolver) error {
	cfg, err := settings.GetUserMcpConfigSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read user mcp config"))
	}
	policy, err := mcp.ParsePolicy(cfg.GetMcpIpPolicy())
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "invalid mcp ip policy"))
	}
	ownerID := int64(0)
	if isPersonal {
		ownerID = 1
	}
	if !policy.AppliesTo(ownerID) {
		return nil
	}

	parsed, err := url.Parse(serverURL)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid MCP server URL %q", serverURL))
	}
	host := parsed.Hostname()
	if host == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid MCP server URL %q", serverURL))
	}

	// IP literal: judge directly, no DNS involved.
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkMcpTargetAddr(policy, host, addr)
	}

	lookupCtx, cancel := context.WithTimeout(ctx, mcpTargetLookupTimeout)
	defer cancel()
	addrs, err := resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil {
		if policy.HasAllowRestriction() {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(
				"cannot resolve MCP target %q against the workspace MCP IP policy allow list", host))
		}
		// Deny-only policy: a host that fails to resolve will fail at connect
		// time anyway; do not block saving.
		return nil
	}
	for _, addr := range addrs {
		if reason, err := policy.Allowed(addr); err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "mcp ip policy check failed"))
		} else if reason != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(
				"MCP target %q resolves to %s which is %s", host, addr, reason.Error()))
		}
	}
	return nil
}

func checkMcpTargetAddr(policy *mcp.CompiledPolicy, host string, addr netip.Addr) error {
	reason, err := policy.Allowed(addr)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "mcp ip policy check failed"))
	}
	if reason == nil {
		return nil
	}
	return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(
		"MCP target %q resolves to %s which is %s", host, addr, reason.Error()))
}
