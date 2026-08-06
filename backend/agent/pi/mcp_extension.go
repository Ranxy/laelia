package pi

import (
	"os"
	"path/filepath"
)

// managedMcpExtensionTemplate is the pi extension that exposes the agent's
// server-managed MCP tools as native pi tools. It reads the localhost proxy URL
// from LAELIA_MCP_PROXY_URL (injected by buildPiEnv), fetches the catalog, and
// forwards tool calls to the daemon proxy.
//
// The factory MUST be async: pi awaits the factory before starting the session,
// so registerTool calls made after the await run against a still-valid pi ctx.
// A sync factory with fire-and-forget async registration races session startup
// and throws "extension ctx is stale after session replacement or reload".
const managedMcpExtensionTemplate = `export default async function (pi: any) {
  const proxyUrl = process.env.LAELIA_MCP_PROXY_URL;
  if (!proxyUrl) return;
  async function loadTools() {
    const res = await fetch(proxyUrl + "/tools", { signal: AbortSignal.timeout(25000) });
    if (!res.ok) return [];
    const data = await res.json();
    return Array.isArray(data.tools) ? data.tools : [];
  }
  try {
    const tools = await loadTools();
    for (const tool of tools) {
      const serverLabel = tool.serverName + (tool.serverDescription ? " - " + tool.serverDescription : "");
      pi.registerTool({
        name: tool.runtimeName,
        label: serverLabel + ": " + (tool.title || tool.toolName),
        description: serverLabel + ": " + (tool.description || ("Call " + tool.toolName + " on " + tool.serverName + ".")),
        promptSnippet: tool.runtimeName + ": " + serverLabel + ": " + (tool.description || ("Call " + tool.toolName + " on " + tool.serverName)),
        parameters: tool.inputSchema || { type: "object" },
        async execute(_toolCallId: any, params: any) {
          const res = await fetch(proxyUrl + "/call", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              mcpServerId: tool.mcpServerId,
              toolName: tool.toolName,
              arguments: params || {},
              expectedConfigVersion: tool.configVersion,
              expectedAssignmentVersion: tool.assignmentVersion
            }),
            signal: AbortSignal.timeout(25000)
          });
          if (!res.ok) throw new Error("managed MCP call failed: HTTP " + res.status);
          const result = await res.json();
          if (result.isError) {
            const text = (result.content || []).filter((b: any) => b.type === "text").map((b: any) => b.text).join("\n") || "Managed MCP tool failed";
            throw new Error(text);
          }
          return {
            content: (result.content || []).map((b: any) => {
              if (b.type === "image") return { type: "image", data: b.data, mimeType: b.mimeType };
              return { type: "text", text: b.text || "" };
            }),
            details: { managedMcp: true, mcpServerId: tool.mcpServerId, toolName: tool.toolName }
          };
        }
      });
    }
  } catch (err) {
    console.warn("laelia managed mcp extension failed to register tools: " + String(err));
  }
}
`

// writeManagedMcpExtension materializes the managed-MCP extension into the
// agent's project-local .pi/extensions directory so pi auto-discovers it at
// session start. No-op when the proxy URL is unset.
func writeManagedMcpExtension(cfg *PiConfig) error {
	if cfg == nil || cfg.McpProxyURL == "" {
		return nil
	}
	dir := filepath.Join(cfg.WorkingDir, ".pi", "extensions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "laelia-managed-mcp.ts"), []byte(managedMcpExtensionTemplate), 0o600)
}
