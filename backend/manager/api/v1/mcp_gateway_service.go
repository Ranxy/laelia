package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/mcp"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// mcpToolCacheTTL bounds how long a fetched MCP tool list is reused.
	mcpToolCacheTTL = 5 * time.Minute
	// maxNativeMcpToolNameLength bounds the runtime tool name length.
	maxNativeMcpToolNameLength = 48
)

type mcpToolCacheEntry struct {
	tools     []mcp.Tool
	fetchedAt time.Time
}

// McpGatewayService is the agent-facing MCP gateway: machines fetch the
// per-agent authorized tool catalog and invoke allowlisted tools through it.
// Transport configuration and header values stay on the manager.
type McpGatewayService struct {
	v1connect.UnimplementedMcpGatewayServiceHandler
	store     *store.Store
	iam       *iam.Manager
	client    *mcp.Client
	cacheMu   sync.Mutex
	toolCache map[string]mcpToolCacheEntry

	// ipPolicyMu guards the compiled MCP IP policy cache; the policy is
	// refreshed at most every ipPolicyCacheTTL so per-call gateway checks do
	// not recompile (or re-read) it constantly.
	ipPolicyMu        sync.Mutex
	ipPolicyCompiled  *mcp.CompiledPolicy
	ipPolicyFetchedAt time.Time
}

// NewMcpGatewayService returns a new McpGatewayService.
func NewMcpGatewayService(s *store.Store, iamManager *iam.Manager) *McpGatewayService {
	svc := &McpGatewayService{
		store:     s,
		iam:       iamManager,
		client:    mcp.New(),
		toolCache: make(map[string]mcpToolCacheEntry),
	}
	svc.client.SetIPPolicy(svc.checkTargetIP)
	return svc
}

// ipPolicyCacheTTL bounds how long the compiled MCP IP policy is reused.
const ipPolicyCacheTTL = 30 * time.Second

// checkTargetIP is the client's IP policy hook: it applies the compiled
// workspace policy to the server's target address, honoring the policy scope.
func (s *McpGatewayService) checkTargetIP(ctx context.Context, server *store.McpServerMessage, ip netip.Addr) (bool, error) {
	policy, err := s.compiledIPPolicy(ctx)
	if err != nil {
		return false, err
	}
	if !policy.AppliesTo(server.OwnerID) {
		return true, nil
	}
	reason, err := policy.Allowed(ip)
	if err != nil {
		return false, err
	}
	return reason == nil, nil
}

// compiledIPPolicy returns the workspace MCP IP policy, cached for
// ipPolicyCacheTTL.
func (s *McpGatewayService) compiledIPPolicy(ctx context.Context) (*mcp.CompiledPolicy, error) {
	s.ipPolicyMu.Lock()
	if s.ipPolicyCompiled != nil && time.Since(s.ipPolicyFetchedAt) < ipPolicyCacheTTL {
		policy := s.ipPolicyCompiled
		s.ipPolicyMu.Unlock()
		return policy, nil
	}
	s.ipPolicyMu.Unlock()

	cfg, err := s.store.GetUserMcpConfigSetting(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := mcp.ParsePolicy(cfg.GetMcpIpPolicy())
	if err != nil {
		return nil, err
	}
	s.ipPolicyMu.Lock()
	s.ipPolicyCompiled = policy
	s.ipPolicyFetchedAt = time.Now()
	s.ipPolicyMu.Unlock()
	return policy, nil
}

// Compile-time assertion that the service implements every RPC of the generated
// connect handler.
var _ v1connect.McpGatewayServiceHandler = (*McpGatewayService)(nil)

// GetMcpCatalog returns the current Server-authorized MCP tool catalog for the
// calling agent. The catalog is recomputed per call from the agent's enabled
// servers, the servers' current tool lists, and the caller's current
// authorization.
func (s *McpGatewayService) GetMcpCatalog(ctx context.Context, _ *connect.Request[v1pb.GetMcpCatalogRequest]) (*connect.Response[v1pb.GetMcpCatalogResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	assignments, err := s.store.ListAgentMcpServers(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list agent mcp servers"))
	}

	response := &v1pb.GetMcpCatalogResponse{CatalogVersion: 1}
	for _, assignment := range assignments {
		server, err := s.store.GetMcpServerByResourceID(ctx, assignment.ServerResourceID)
		if err != nil {
			slog.Warn("failed to load mcp server for catalog", "agent", agent.ResourceID, "server", assignment.ServerResourceID, "error", err)
			continue
		}
		if server == nil {
			continue
		}
		authorized, err := agentCanUseMcpServer(ctx, s.store, s.iam, agent, server)
		if err != nil {
			slog.Warn("failed to check mcp server access for catalog", "agent", agent.ResourceID, "server", assignment.ServerResourceID, "error", err)
			continue
		}
		if !authorized {
			continue
		}
		tools, err := s.toolsForServer(ctx, server)
		if err != nil {
			slog.Warn("failed to list mcp server tools", "agent", agent.ResourceID, "server", assignment.ServerResourceID, "error", err)
			continue
		}
		for _, tool := range tools {
			if strings.TrimSpace(tool.Name) == "" {
				continue
			}
			response.Tools = append(response.Tools, &v1pb.McpTool{
				McpServerId:       common.FormatMcpServerUID(server.ResourceID),
				ServerName:        server.Title,
				ServerDescription: server.Description,
				ToolName:          tool.Name,
				RuntimeName:       nativeMcpToolName(server.ResourceID, tool.Name),
				Title:             tool.Title,
				Description:       tool.Description,
				InputSchema:       toStruct(tool.InputSchema),
				ConfigVersion:     server.ConfigVersion,
				AssignmentVersion: assignment.AssignmentVersion,
			})
		}
	}
	return connect.NewResponse(response), nil
}

// CallMcpTool invokes an allowlisted managed MCP tool through the gateway. The
// caller's current authorization and the expected config/assignment versions
// are re-checked before the call.
func (s *McpGatewayService) CallMcpTool(ctx context.Context, req *connect.Request[v1pb.CallMcpToolRequest]) (*connect.Response[v1pb.CallMcpToolResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}
	serverResourceID, err := common.GetMcpServerResourceID(req.Msg.McpServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	assignments, err := s.store.ListAgentMcpServers(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list agent mcp servers"))
	}
	var assignment *store.AgentMcpMessage
	for i := range assignments {
		if assignments[i].ServerResourceID == serverResourceID {
			assignment = &assignments[i]
			break
		}
	}
	if assignment == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q is not enabled on this agent", req.Msg.McpServerId))
	}

	server, err := s.store.GetMcpServerByResourceID(ctx, serverResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get mcp server"))
	}
	if server == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q not found", req.Msg.McpServerId))
	}
	authorized, err := agentCanUseMcpServer(ctx, s.store, s.iam, agent, server)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check mcp server access"))
	}
	if !authorized {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("this agent is no longer allowed to use mcp server %q", req.Msg.McpServerId))
	}

	if req.Msg.ExpectedConfigVersion != 0 && req.Msg.ExpectedConfigVersion != server.ConfigVersion {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("mcp catalog is stale: mcp_stale_catalog"))
	}
	if req.Msg.ExpectedAssignmentVersion != 0 && req.Msg.ExpectedAssignmentVersion != assignment.AssignmentVersion {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("mcp catalog is stale: mcp_stale_catalog"))
	}

	tools, err := s.toolsForServer(ctx, server)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list mcp server tools"))
	}
	var tool *mcp.Tool
	for i := range tools {
		if tools[i].Name == req.Msg.ToolName {
			tool = &tools[i]
			break
		}
	}
	if tool == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("tool %q is not in the current mcp catalog", req.Msg.ToolName))
	}

	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	result, err := s.client.CallTool(callCtx, server, tool.Name, req.Msg.Arguments.AsMap())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "mcp tool call failed"))
	}
	return connect.NewResponse(convertCallResult(result)), nil
}

// toolsForServer returns the server's tool list, cached for mcpToolCacheTTL
// and keyed by server resource id + config version.
func (s *McpGatewayService) toolsForServer(ctx context.Context, server *store.McpServerMessage) ([]mcp.Tool, error) {
	key := server.ResourceID + ":" + strconv.FormatInt(server.ConfigVersion, 10)
	s.cacheMu.Lock()
	if entry, ok := s.toolCache[key]; ok && time.Since(entry.fetchedAt) < mcpToolCacheTTL {
		s.cacheMu.Unlock()
		return entry.tools, nil
	}
	s.cacheMu.Unlock()

	listCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	tools, err := s.client.ListTools(listCtx, server)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	s.toolCache[key] = mcpToolCacheEntry{tools: tools, fetchedAt: time.Now()}
	s.cacheMu.Unlock()
	return tools, nil
}

// agentCanUseMcpServer reports whether the agent's owner (or creator for
// legacy agents) may use the server. Authorization is re-checked on every
// catalog fetch and tool call, so removing a user/group from a server's
// members takes effect immediately.
func agentCanUseMcpServer(ctx context.Context, stores *store.Store, iamManager *iam.Manager, agent *store.AgentMessage, server *store.McpServerMessage) (bool, error) {
	userID := agent.OwnerID
	if userID == 0 {
		userID = agent.CreatedBy
	}
	if userID == 0 {
		return false, nil
	}
	user, err := stores.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return canUseMcpServer(ctx, iamManager, stores, user, server)
}

// nativeMcpToolName builds a collision-scoped runtime tool name:
// r<sha256(serverID)[:8]>_<sanitized tool name>.
func nativeMcpToolName(serverResourceID, toolName string) string {
	sum := sha256.Sum256([]byte(serverResourceID))
	readable := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, toolName)
	if readable == "" {
		readable = "tool"
	}
	prefix := "r" + hex.EncodeToString(sum[:4]) + "_"
	remain := maxNativeMcpToolNameLength - len(prefix)
	if len(readable) > remain {
		readable = readable[:remain]
	}
	return prefix + readable
}

// toStruct converts a JSON-schema-ish map into a protobuf Struct.
func toStruct(in map[string]any) *structpb.Struct {
	if len(in) == 0 {
		return nil
	}
	out, err := structpb.NewStruct(in)
	if err != nil {
		return nil
	}
	return out
}

// convertCallResult normalizes an MCP tools/call result into the v1 response.
func convertCallResult(result *mcp.CallResult) *v1pb.CallMcpToolResponse {
	out := &v1pb.CallMcpToolResponse{IsError: result.IsError}
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			out.Content = append(out.Content, &v1pb.McpContentBlock{
				Kind: &v1pb.McpContentBlock_Text{Text: &v1pb.McpTextContent{Text: block.Text}},
			})
		case "image":
			out.Content = append(out.Content, &v1pb.McpContentBlock{
				Kind: &v1pb.McpContentBlock_Image{Image: &v1pb.McpImageContent{Data: block.Data, MimeType: block.MimeType}},
			})
		default:
		}
	}
	if len(result.StructuredContent) > 0 {
		out.StructuredContent = toStruct(result.StructuredContent)
	}
	return out
}
