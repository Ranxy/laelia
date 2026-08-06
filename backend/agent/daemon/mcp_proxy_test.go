package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

type fakeMcpGatewayClient struct {
	catalog *v1pb.GetMcpCatalogResponse
	call    *v1pb.CallMcpToolResponse
	gotCall *v1pb.CallMcpToolRequest
}

func (f *fakeMcpGatewayClient) GetMcpCatalog(_ context.Context, _ *connect.Request[v1pb.GetMcpCatalogRequest]) (*connect.Response[v1pb.GetMcpCatalogResponse], error) {
	return connect.NewResponse(f.catalog), nil
}

func (f *fakeMcpGatewayClient) CallMcpTool(_ context.Context, req *connect.Request[v1pb.CallMcpToolRequest]) (*connect.Response[v1pb.CallMcpToolResponse], error) {
	f.gotCall = req.Msg
	return connect.NewResponse(f.call), nil
}

func TestHandleMcpProxyRoutesToGateway(t *testing.T) {
	srv, err := New("http://manager.test", "machine-1", func() string { return "machine-token" }, http.DefaultClient)
	require.NoError(t, err)
	srv.mcpProxyTokens["tok"] = "agents/abc"
	srv.mcpProxyAgents["agents/abc"] = "tok"
	fake := &fakeMcpGatewayClient{
		catalog: &v1pb.GetMcpCatalogResponse{
			CatalogVersion: 1,
			Tools: []*v1pb.McpTool{
				{
					McpServerId:       "mcpServers/srv-1",
					ServerName:        "GitHub",
					ServerDescription: "GitHub tools",
					ToolName:          "do_it",
					RuntimeName:       "r123_do_it",
					Description:       "does it",
					ConfigVersion:     1,
					AssignmentVersion: 2,
				},
			},
		},
		call: &v1pb.CallMcpToolResponse{
			Content: []*v1pb.McpContentBlock{
				{Kind: &v1pb.McpContentBlock_Text{Text: &v1pb.McpTextContent{Text: "ok"}}},
			},
		},
	}
	srv.mcpClients["abc"] = fake

	ts := httptest.NewServer(http.HandlerFunc(srv.handleMcpProxy))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp/tok/tools")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var catalog struct {
		CatalogVersion int `json:"catalogVersion"`
		Tools          []struct {
			McpServerID       string `json:"mcpServerId"`
			ServerName        string `json:"serverName"`
			ServerDescription string `json:"serverDescription"`
			ToolName          string `json:"toolName"`
			RuntimeName       string `json:"runtimeName"`
			ConfigVersion     int64  `json:"configVersion"`
			AssignmentVersion int64  `json:"assignmentVersion"`
		} `json:"tools"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&catalog))
	require.Equal(t, 1, catalog.CatalogVersion)
	require.Len(t, catalog.Tools, 1)
	assert.Equal(t, "mcpServers/srv-1", catalog.Tools[0].McpServerID)
	assert.Equal(t, "GitHub", catalog.Tools[0].ServerName)
	assert.Equal(t, "GitHub tools", catalog.Tools[0].ServerDescription)
	assert.Equal(t, "r123_do_it", catalog.Tools[0].RuntimeName)
	assert.EqualValues(t, 1, catalog.Tools[0].ConfigVersion)
	assert.EqualValues(t, 2, catalog.Tools[0].AssignmentVersion)

	callResp, err := http.Post(
		ts.URL+"/mcp/tok/call",
		"application/json",
		strings.NewReader(`{
			"mcpServerId": "mcpServers/srv-1",
			"toolName": "do_it",
			"arguments": {"x": 1},
			"expectedConfigVersion": 1,
			"expectedAssignmentVersion": 2
		}`),
	)
	require.NoError(t, err)
	defer callResp.Body.Close()
	require.Equal(t, http.StatusOK, callResp.StatusCode)
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	require.NoError(t, json.NewDecoder(callResp.Body).Decode(&result))
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "ok", result.Content[0].Text)
	assert.False(t, result.IsError)

	require.NotNil(t, fake.gotCall)
	assert.Equal(t, "mcpServers/srv-1", fake.gotCall.McpServerId)
	assert.Equal(t, "do_it", fake.gotCall.ToolName)
	assert.EqualValues(t, 1, fake.gotCall.ExpectedConfigVersion)
	assert.EqualValues(t, 2, fake.gotCall.ExpectedAssignmentVersion)
	assert.EqualValues(t, 1, fake.gotCall.Arguments.AsMap()["x"])
}

var _ v1connect.McpGatewayServiceClient = (*fakeMcpGatewayClient)(nil)
