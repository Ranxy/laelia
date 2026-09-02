package daemon

import (
	"context"

	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// McpTools fetches the server-authorized MCP tool catalog for the agent
// identified by the given agents/{id} resource name.
func (s *Server) McpTools(ctx context.Context, agent string) (*v1pb.GetMcpCatalogResponse, error) {
	resp, err := s.mcpClient(bareAgentID(agent)).GetMcpCatalog(ctx, connect.NewRequest(&v1pb.GetMcpCatalogRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// McpCall forwards one managed MCP tool call to the manager gateway.
func (s *Server) McpCall(ctx context.Context, agent string, req *v1pb.CallMcpToolRequest) (*v1pb.CallMcpToolResponse, error) {
	resp, err := s.mcpClient(bareAgentID(agent)).CallMcpTool(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// McpProxyURLForAgent returns the localhost TCP proxy URL the agent's pi
// extension calls to fetch the managed MCP catalog and invoke tools. The URL is
// stable per agent for the daemon lifetime and is authenticated by an
// unguessable per-agent token.
func (s *Server) McpProxyURLForAgent(agent string) (string, error) {
	s.mcpProxyMu.Lock()
	defer s.mcpProxyMu.Unlock()
	if s.mcpProxyServer == nil {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", errors.Wrap(err, "bind mcp proxy")
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", s.handleMcpProxy)
		s.mcpProxyServer = &http.Server{Handler: mux}
		s.mcpProxyBase = "http://" + listener.Addr().String()
		go func() {
			if err := s.mcpProxyServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				slog.Error("mcp proxy serve error", "error", err)
			}
		}()
	}
	if token, ok := s.mcpProxyAgents[agent]; ok {
		return s.mcpProxyBase + "/mcp/" + token, nil
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", errors.Wrap(err, "generate mcp proxy token")
	}
	token := hex.EncodeToString(tokenBytes)
	s.mcpProxyTokens[token] = agent
	s.mcpProxyAgents[agent] = token
	return s.mcpProxyBase + "/mcp/" + token, nil
}

// handleMcpProxy serves the localhost REST proxy for pi extensions:
// GET /mcp/{token}/tools and POST /mcp/{token}/call.
func (s *Server) handleMcpProxy(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "mcp" {
		http.NotFound(w, r)
		return
	}
	s.mcpProxyMu.Lock()
	agent := s.mcpProxyTokens[parts[1]]
	s.mcpProxyMu.Unlock()
	if agent == "" {
		http.NotFound(w, r)
		return
	}

	switch parts[2] {
	case "tools":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		catalog, err := s.McpTools(r.Context(), agent)
		if err != nil {
			mcpProxyError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeMcpCatalogJSON(w, catalog)
	case "call":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			McpServerID               string          `json:"mcpServerId"`
			ToolName                  string          `json:"toolName"`
			Arguments                 json.RawMessage `json:"arguments"`
			ExpectedConfigVersion     int64           `json:"expectedConfigVersion"`
			ExpectedAssignmentVersion int64           `json:"expectedAssignmentVersion"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			mcpProxyError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		args := &structpb.Struct{}
		if len(req.Arguments) > 0 {
			if err := common.ProtojsonUnmarshaler.Unmarshal(req.Arguments, args); err != nil {
				mcpProxyError(w, http.StatusBadRequest, "invalid arguments: "+err.Error())
				return
			}
		}
		result, err := s.McpCall(r.Context(), agent, &v1pb.CallMcpToolRequest{
			McpServerId:               req.McpServerID,
			ToolName:                  req.ToolName,
			Arguments:                 args,
			ExpectedConfigVersion:     req.ExpectedConfigVersion,
			ExpectedAssignmentVersion: req.ExpectedAssignmentVersion,
		})
		if err != nil {
			mcpProxyError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeMcpCallResultJSON(w, result)
	default:
		http.NotFound(w, r)
	}
}

// writeJSON writes a JSON payload with MCP wire naming.
func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// writeMcpCatalogJSON writes a GetMcpCatalogResponse as the JSON shape the
// managed-MCP consumers (pi extension and ACP mcp-proxy) expect. protojson is
// not used because it serializes int64 version fields as strings; consumers
// expect numbers.
func writeMcpCatalogJSON(w http.ResponseWriter, catalog *v1pb.GetMcpCatalogResponse) {
	tools := make([]map[string]any, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		entry := map[string]any{
			"mcpServerId":       tool.McpServerId,
			"serverName":        tool.ServerName,
			"toolName":          tool.ToolName,
			"runtimeName":       tool.RuntimeName,
			"title":             tool.Title,
			"description":       tool.Description,
			"configVersion":     tool.ConfigVersion,
			"assignmentVersion": tool.AssignmentVersion,
		}
		if tool.ServerDescription != "" {
			entry["serverDescription"] = tool.ServerDescription
		}
		if tool.InputSchema != nil {
			entry["inputSchema"] = tool.InputSchema.AsMap()
		}
		tools = append(tools, entry)
	}
	writeJSON(w, map[string]any{"catalogVersion": catalog.CatalogVersion, "tools": tools})
}

// writeMcpCallResultJSON writes a CallMcpToolResponse as MCP wire content
// blocks (explicit type), the shape the pi extension and ACP mcp-proxy expect.
// protojson would nest blocks as {"text":{"text":"..."}} and drop the type.
func writeMcpCallResultJSON(w http.ResponseWriter, result *v1pb.CallMcpToolResponse) {
	blocks := make([]map[string]any, 0, len(result.Content))
	for _, block := range result.Content {
		switch kind := block.Kind.(type) {
		case *v1pb.McpContentBlock_Text:
			blocks = append(blocks, map[string]any{"type": "text", "text": kind.Text.GetText()})
		case *v1pb.McpContentBlock_Image:
			blocks = append(blocks, map[string]any{"type": "image", "data": kind.Image.GetData(), "mimeType": kind.Image.GetMimeType()})
		default:
		}
	}
	payload := map[string]any{"content": blocks, "isError": result.IsError}
	if result.StructuredContent != nil {
		payload["structuredContent"] = result.StructuredContent.AsMap()
	}
	writeJSON(w, payload)
}

func mcpProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (s *Server) handleMcpTools(w http.ResponseWriter, r *http.Request) {
	if e := s.authorize(r); e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: "failed to decode request body: " + err.Error()})
		return
	}
	catalog, err := s.McpTools(r.Context(), req.Agent)
	if err != nil {
		writeError(w, &chattools.Error{Code: "MCP_GATEWAY_FAILED", Message: err.Error()})
		return
	}
	writeMcpCatalogJSON(w, catalog)
}

func (s *Server) handleMcpCall(w http.ResponseWriter, r *http.Request) {
	if e := s.authorize(r); e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Agent                     string          `json:"agent"`
		McpServerID               string          `json:"mcp_server_id"`
		ToolName                  string          `json:"tool_name"`
		Arguments                 json.RawMessage `json:"arguments"`
		ExpectedConfigVersion     int64           `json:"expected_config_version"`
		ExpectedAssignmentVersion int64           `json:"expected_assignment_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: "failed to decode request body: " + err.Error()})
		return
	}
	args := &structpb.Struct{}
	if len(req.Arguments) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(req.Arguments, args); err != nil {
			writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: "invalid arguments: " + err.Error()})
			return
		}
	}
	result, err := s.McpCall(r.Context(), req.Agent, &v1pb.CallMcpToolRequest{
		McpServerId:               req.McpServerID,
		ToolName:                  req.ToolName,
		Arguments:                 args,
		ExpectedConfigVersion:     req.ExpectedConfigVersion,
		ExpectedAssignmentVersion: req.ExpectedAssignmentVersion,
	})
	if err != nil {
		writeError(w, &chattools.Error{Code: "MCP_GATEWAY_FAILED", Message: err.Error()})
		return
	}
	writeMcpCallResultJSON(w, result)
}
