package v1

import (
	"strings"
	"testing"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/mcp"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestValidateMcpServerBase(t *testing.T) {
	if err := validateMcpServerBase(&v1pb.McpServer{Title: "x", Transport: &v1pb.McpServer_Http{Http: &v1pb.McpHttpTransport{Url: "https://mcp.example.com"}}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateMcpServerBase(&v1pb.McpServer{Transport: &v1pb.McpServer_Sse{Sse: &v1pb.McpSseTransport{Url: "https://mcp.example.com/sse"}}}); err == nil {
		t.Fatal("expected missing title to be rejected")
	}
	if err := validateMcpServerBase(&v1pb.McpServer{Title: "x"}); err == nil {
		t.Fatal("expected missing transport to be rejected")
	}
}

func TestBuildMcpTransportForCreate(t *testing.T) {
	in := &v1pb.McpServer{
		Transport: &v1pb.McpServer_Http{
			Http: &v1pb.McpHttpTransport{
				Url: "https://mcp.example.com/mcp",
				Headers: []*v1pb.McpHeader{
					{Name: "Authorization", Value: "Bearer secret"},
				},
			},
		},
	}
	transportType, serverURL, headers, err := buildMcpTransportForCreate(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transportType != "http" || serverURL != "https://mcp.example.com/mcp" {
		t.Fatalf("got %q %q", transportType, serverURL)
	}
	if headers["Authorization"] != "Bearer secret" {
		t.Fatalf("unexpected headers: %v", headers)
	}
	if _, _, _, err := buildMcpTransportForCreate(&v1pb.McpServer{
		Transport: &v1pb.McpServer_Http{Http: &v1pb.McpHttpTransport{Url: "ftp://mcp.example.com"}},
	}); err == nil {
		t.Fatal("expected non-http URL to be rejected")
	}
}

func TestResolveMcpHeaders(t *testing.T) {
	stored := map[string]string{"Authorization": "Bearer old", "X-Api-Key": "k"}
	incoming := map[string]string{
		"Authorization": "****1234", // masked -> keep stored
		"X-Api-Key":     "new-key",  // real value -> replace
		"X-Dropped":     "",
	}
	got := resolveMcpHeaders(stored, incoming)
	if got["Authorization"] != "Bearer old" {
		t.Fatalf("masked value should keep stored: %v", got)
	}
	if got["X-Api-Key"] != "new-key" {
		t.Fatalf("real value should replace: %v", got)
	}
	if _, ok := got["X-Dropped"]; ok {
		t.Fatalf("empty value should be dropped: %v", got)
	}
}

func TestValidateMcpServerUpdateMask(t *testing.T) {
	if err := validateMcpServerUpdateMask([]string{"title", "http", "members"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateMcpServerUpdateMask([]string{"name"}); err == nil {
		t.Fatal("expected immutable field name to be rejected")
	}
}

func TestNativeMcpToolName(t *testing.T) {
	name := nativeMcpToolName("server-1", "do_stuff")
	if !strings.HasPrefix(name, "r") || !strings.Contains(name, "_") || !strings.HasSuffix(name, "do_stuff") {
		t.Fatalf("unexpected tool name %q", name)
	}
	if len(name) > maxNativeMcpToolNameLength {
		t.Fatalf("tool name too long: %d", len(name))
	}
	first := nativeMcpToolName("server-1", "do_stuff")
	if first != nativeMcpToolName("server-1", "do_stuff") {
		t.Fatal("tool name must be deterministic")
	}
	sanitized := nativeMcpToolName("server-1", "a b/c")
	if sanitized != nativeMcpToolName("server-1", "a b/c") {
		t.Fatal("sanitized tool name must be deterministic")
	}
}

func TestConvertCallResult(t *testing.T) {
	result := &mcp.CallResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "image", Data: "aGk=", MimeType: "image/png"},
			{Type: "resource"}, // unsupported -> dropped
		},
		IsError:           false,
		StructuredContent: map[string]any{"x": "y"},
	}
	out := convertCallResult(result)
	if len(out.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(out.Content))
	}
	if out.Content[0].GetText() == nil || out.Content[0].GetText().Text != "hello" {
		t.Fatalf("unexpected text block: %v", out.Content[0])
	}
	if out.Content[1].GetImage() == nil || out.Content[1].GetImage().Data != "aGk=" {
		t.Fatalf("unexpected image block: %v", out.Content[1])
	}
	if out.StructuredContent == nil || out.StructuredContent.AsMap()["x"] != "y" {
		t.Fatalf("unexpected structured content: %v", out.StructuredContent)
	}
}

func TestConvertToV1McpServerMasksHeaders(t *testing.T) {
	server := &store.McpServerMessage{
		ResourceID:    "abc",
		Title:         "Test",
		TransportType: "http",
		URL:           "https://mcp.example.com",
		Headers:       map[string]string{"Authorization": "secret-1234"},
		Members:       []string{"users/101"},
	}
	out := convertToV1McpServer(server)
	if out.Name != "mcpServers/abc" {
		t.Fatalf("unexpected name %q", out.Name)
	}
	httpTransport := out.GetHttp()
	if httpTransport == nil || httpTransport.Url != server.URL {
		t.Fatalf("unexpected transport: %v", out.Transport)
	}
	if len(httpTransport.Headers) != 1 || httpTransport.Headers[0].MaskedValue == "" || httpTransport.Headers[0].Value != "" {
		t.Fatalf("header must be masked: %v", httpTransport.Headers)
	}
}
