package v1

import (
	"context"
	"net/url"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/Ranxy/laelia/backend/manager/utils"
)

// McpServerService manages the workspace MCP server registry. Management RPCs
// are gated by the IAM interceptor with laelia.mcpServers.*. ListMcpServers is
// handler-gated so the agent config form can list the servers the caller may
// use without a management permission.
type McpServerService struct {
	v1connect.UnimplementedMcpServerServiceHandler
	store *store.Store
	iam   *iam.Manager
}

// NewMcpServerService returns a new McpServerService.
func NewMcpServerService(s *store.Store, iamManager *iam.Manager) *McpServerService {
	return &McpServerService{store: s, iam: iamManager}
}

// Compile-time assertion that the service implements every RPC of the generated
// connect handler.
var _ v1connect.McpServerServiceHandler = (*McpServerService)(nil)

// GetMcpServer returns one MCP server. Management-only
// (laelia.mcpServers.get).
func (s *McpServerService) GetMcpServer(ctx context.Context, req *connect.Request[v1pb.GetMcpServerRequest]) (*connect.Response[v1pb.McpServer], error) {
	resourceID, err := common.GetMcpServerResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	server, err := s.store.GetMcpServerByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get mcp server"))
	}
	if server == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q not found", req.Msg.Name))
	}
	return connect.NewResponse(convertToV1McpServer(server)), nil
}

// ListMcpServers lists MCP servers. It is handler-gated (no IAM annotation): a
// caller holding laelia.mcpServers.list sees every server; any other caller
// sees only the servers they may use.
func (s *McpServerService) ListMcpServers(ctx context.Context, _ *connect.Request[v1pb.ListMcpServersRequest]) (*connect.Response[v1pb.ListMcpServersResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	servers, err := s.store.ListMcpServers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list mcp servers"))
	}

	response := &v1pb.ListMcpServersResponse{}
	for _, server := range servers {
		ok, err := canUseMcpServer(ctx, s.iam, s.store, user, server)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve mcp server access"))
		}
		if !ok {
			continue
		}
		response.McpServers = append(response.McpServers, convertToV1McpServer(server))
	}
	return connect.NewResponse(response), nil
}

// CreateMcpServer creates an MCP server with its members. Header values are
// required at creation time.
func (s *McpServerService) CreateMcpServer(ctx context.Context, req *connect.Request[v1pb.CreateMcpServerRequest]) (*connect.Response[v1pb.McpServer], error) {
	in := req.Msg.McpServer
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mcp_server is required"))
	}
	if err := validateMcpServerBase(in); err != nil {
		return nil, err
	}
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	transportType, serverURL, headers, err := buildMcpTransportForCreate(in)
	if err != nil {
		return nil, err
	}
	members, err := validateAndNormalizeMembers(in.Members)
	if err != nil {
		return nil, err
	}

	created, err := s.store.CreateMcpServer(ctx, &store.McpServerMessage{
		Title:         strings.TrimSpace(in.Title),
		Description:   strings.TrimSpace(in.Description),
		TransportType: transportType,
		URL:           serverURL,
		Headers:       headers,
		CreatedBy:     user.ID,
		Members:       members,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create mcp server"))
	}

	recordMcpServerChange(ctx, common.FormatMcpServerUID(created.ResourceID))
	return connect.NewResponse(convertToV1McpServer(created)), nil
}

// UpdateMcpServer replaces the server's mutable fields and members (full
// replace). Masked ("****"-prefixed) or empty header values on existing headers
// mean "keep the stored value".
func (s *McpServerService) UpdateMcpServer(ctx context.Context, req *connect.Request[v1pb.UpdateMcpServerRequest]) (*connect.Response[v1pb.McpServer], error) {
	in := req.Msg.McpServer
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("mcp_server is required"))
	}
	if err := validateMcpServerUpdateMask(req.Msg.UpdateMask.GetPaths()); err != nil {
		return nil, err
	}
	resourceID, err := common.GetMcpServerResourceID(in.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	current, err := s.store.GetMcpServerByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get mcp server"))
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q not found", in.Name))
	}
	if err := validateMcpServerBase(in); err != nil {
		return nil, err
	}

	transportType, serverURL, headers, err := buildMcpTransportForUpdate(current, in)
	if err != nil {
		return nil, err
	}
	members, err := validateAndNormalizeMembers(in.Members)
	if err != nil {
		return nil, err
	}

	updated, err := s.store.UpdateMcpServer(ctx, current, &store.McpServerMessage{
		Title:         strings.TrimSpace(in.Title),
		Description:   strings.TrimSpace(in.Description),
		TransportType: transportType,
		URL:           serverURL,
		Headers:       headers,
		Members:       members,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update mcp server"))
	}

	recordMcpServerChange(ctx, in.Name)
	return connect.NewResponse(convertToV1McpServer(updated)), nil
}

// DeleteMcpServer deletes an MCP server. Servers still enabled on an agent are
// rejected with FailedPrecondition.
func (s *McpServerService) DeleteMcpServer(ctx context.Context, req *connect.Request[v1pb.DeleteMcpServerRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetMcpServerResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	server, err := s.store.GetMcpServerByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get mcp server"))
	}
	if server == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q not found", req.Msg.Name))
	}
	count, err := s.store.CountAgentsReferencingMcpServer(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count referencing agents"))
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"mcp server %q is enabled on %d agent(s); reconfigure them before deleting it", req.Msg.Name, count))
	}
	if err := s.store.DeleteMcpServer(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete mcp server"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// canUseMcpServer reports whether the caller may use a server: a caller holding
// laelia.mcpServers.list may use any server; otherwise the caller must be a
// member of the server's member list (users/{uid}, groups/{email|id}, or
// allUsers).
func canUseMcpServer(ctx context.Context, iamChecker *iam.Manager, stores *store.Store, user *store.UserMessage, server *store.McpServerMessage) (bool, error) {
	if user == nil {
		return false, nil
	}
	ok, err := iamChecker.CheckPermission(ctx, permission.McpServersList, user, nil, nil)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	for _, member := range server.Members {
		if utils.MemberContainsUser(ctx, stores, member, user) {
			return true, nil
		}
	}
	return false, nil
}

// validateMcpServerBase validates the server identity fields shared by create
// and update.
func validateMcpServerBase(in *v1pb.McpServer) error {
	if strings.TrimSpace(in.Title) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("title is required"))
	}
	transport := in.GetTransport()
	if transport == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("transport is required (http or sse)"))
	}
	return nil
}

func buildMcpTransportForCreate(in *v1pb.McpServer) (transportType, serverURL string, headers map[string]string, err error) {
	transport := in.GetTransport()
	switch t := transport.(type) {
	case *v1pb.McpServer_Http:
		u, h, err := validateMcpURL(t.Http.GetUrl(), headersFromV1(t.Http.GetHeaders()))
		return "http", u, h, err
	case *v1pb.McpServer_Sse:
		u, h, err := validateMcpURL(t.Sse.GetUrl(), headersFromV1(t.Sse.GetHeaders()))
		return "sse", u, h, err
	default:
		return "", "", nil, connect.NewError(connect.CodeInvalidArgument, errors.New("transport must be http or sse"))
	}
}

func buildMcpTransportForUpdate(current *store.McpServerMessage, in *v1pb.McpServer) (transportType, serverURL string, headers map[string]string, err error) {
	transport := in.GetTransport()
	switch t := transport.(type) {
	case *v1pb.McpServer_Http:
		u, h, err := validateMcpURL(t.Http.GetUrl(), resolveMcpHeaders(current.Headers, headersFromV1(t.Http.GetHeaders())))
		return "http", u, h, err
	case *v1pb.McpServer_Sse:
		u, h, err := validateMcpURL(t.Sse.GetUrl(), resolveMcpHeaders(current.Headers, headersFromV1(t.Sse.GetHeaders())))
		return "sse", u, h, err
	default:
		return "", "", nil, connect.NewError(connect.CodeInvalidArgument, errors.New("transport must be http or sse"))
	}
}

// validateMcpURL checks the URL is an http(s) URL and validates the headers,
// returning the trimmed URL.
func validateMcpURL(rawURL string, headers map[string]string) (string, map[string]string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid MCP server URL %q", rawURL))
	}
	for name, value := range headers {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, ":\r\n") {
			return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid header name %q", name))
		}
		if strings.ContainsAny(value, "\r\n") {
			return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid header value for %q", name))
		}
	}
	return rawURL, headers, nil
}

func headersFromV1(in []*v1pb.McpHeader) map[string]string {
	out := make(map[string]string, len(in))
	for _, h := range in {
		name := strings.TrimSpace(h.GetName())
		if name == "" {
			continue
		}
		out[name] = h.GetValue()
	}
	return out
}

// resolveMcpHeaders merges the incoming headers with the stored ones: an empty
// or masked value keeps the stored value for that header name; otherwise the
// value replaces it. Headers absent from the incoming list are dropped (full
// replace).
func resolveMcpHeaders(stored map[string]string, incoming map[string]string) map[string]string {
	out := make(map[string]string, len(incoming))
	for name, value := range incoming {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, secretMaskPrefix) {
			if existing, ok := stored[name]; ok {
				out[name] = existing
			}
			continue
		}
		out[name] = value
	}
	return out
}

// validateMcpServerUpdateMask restricts the update mask to the mutable fields.
// An empty mask updates everything mutable (members are full-replace).
func validateMcpServerUpdateMask(paths []string) error {
	allowed := map[string]bool{
		"title":       true,
		"description": true,
		"http":        true,
		"sse":         true,
		"members":     true,
	}
	for _, p := range paths {
		if !allowed[p] {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("update_mask path %q is not supported", p))
		}
	}
	return nil
}

// convertToV1McpServer converts a stored server to the v1 view. Header values
// are masked; the values themselves never cross the API.
func convertToV1McpServer(p *store.McpServerMessage) *v1pb.McpServer {
	out := &v1pb.McpServer{
		Name:          common.FormatMcpServerUID(p.ResourceID),
		Title:         p.Title,
		Description:   p.Description,
		Members:       append([]string(nil), p.Members...),
		CreatedAt:     timestamppb.New(p.CreatedAt),
		UpdatedAt:     timestamppb.New(p.UpdatedAt),
		CreatedBy:     common.FormatUserUID(p.CreatedBy),
		ConfigVersion: p.ConfigVersion,
	}
	headers := make([]*v1pb.McpHeader, 0, len(p.Headers))
	for name, value := range p.Headers {
		headers = append(headers, &v1pb.McpHeader{
			Name:        name,
			MaskedValue: maskSecret(value),
		})
	}
	switch p.TransportType {
	case "http":
		out.Transport = &v1pb.McpServer_Http{Http: &v1pb.McpHttpTransport{Url: p.URL, Headers: headers}}
	case "sse":
		out.Transport = &v1pb.McpServer_Sse{Sse: &v1pb.McpSseTransport{Url: p.URL, Headers: headers}}
	default:
	}
	return out
}

// recordMcpServerChange attaches a masked change summary to the audit record
// the interceptor writes. It carries the server resource name only — never
// header values.
func recordMcpServerChange(ctx context.Context, server string) {
	setServiceData, ok := common.GetSetServiceDataFromContext(ctx)
	if !ok {
		return
	}
	a, err := anypb.New(&v1pb.McpServerChange{Server: server})
	if err != nil {
		return
	}
	setServiceData(a)
}
