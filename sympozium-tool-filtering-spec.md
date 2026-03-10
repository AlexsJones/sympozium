# Feature: MCP Tool Filtering

## Problem
The MCP bridge exposes ALL tools from every MCP server to the agent. For k8s-networking that's 42 tools. Each tool definition costs ~300 tokens. That's ~12K tokens burned on tool schemas alone, before the agent even starts thinking. Most runs only need 10-12 tools.

## Solution
Add tool filtering at two levels:

### 1. MCPServer CRD — `toolsAllow` / `toolsDeny` fields
Add optional allow/deny lists to the MCPServer spec. These filter which tools are exposed from that MCP server.

In `api/v1alpha1/mcpserver_types.go`, add to `MCPServerSpec`:
```go
// ToolsAllow lists tool names (without prefix) to expose. If set, only these tools are registered.
// +optional
ToolsAllow []string `json:"toolsAllow,omitempty"`

// ToolsDeny lists tool names (without prefix) to hide. Applied after toolsAllow.
// +optional  
ToolsDeny []string `json:"toolsDeny,omitempty"`
```

Logic: if `toolsAllow` is non-empty, only those tools pass. Then `toolsDeny` removes any remaining. This lets you do either allowlist or denylist approaches.

### 2. MCP Bridge ServerConfig — same fields
In `internal/mcpbridge/types.go`, add to `ServerConfig`:
```go
ToolsAllow []string `yaml:"toolsAllow,omitempty"`
ToolsDeny  []string `yaml:"toolsDeny,omitempty"`
```

### 3. Controller passes filter to bridge config
In the MCPServer controller (`internal/controller/mcpserver_controller.go`) or the pod builder (`internal/orchestrator/podbuilder.go`), when generating the MCP bridge config YAML for the sidecar, propagate `toolsAllow`/`toolsDeny` from the MCPServer CR spec into the bridge ServerConfig.

### 4. Bridge filters during tool discovery
In the bridge code where it calls `tools/list` on the upstream MCP server and builds the `MCPToolManifest` (likely in `internal/mcpbridge/bridge.go` or `client.go`), filter the tools list:

```go
func filterTools(tools []MCPTool, allow, deny []string) []MCPTool {
    if len(allow) == 0 && len(deny) == 0 {
        return tools
    }
    allowSet := toSet(allow)
    denySet := toSet(deny)
    var filtered []MCPTool
    for _, t := range tools {
        if len(allowSet) > 0 && !allowSet[t.Name] {
            continue
        }
        if denySet[t.Name] {
            continue
        }
        filtered = append(filtered, t)
    }
    return filtered
}
```

Apply this filter BEFORE writing the tool manifest to `/ipc/tools/mcp-tools.json`.

### 5. Also filter incoming tool calls
When the bridge receives an MCPRequest, if the tool (after stripping prefix) is not in the filtered set, return an error immediately instead of forwarding to the upstream server. Defense in depth.

## Testing
- Unit test for `filterTools` — allow only, deny only, both, neither
- Unit test for bridge rejecting filtered-out tool calls
- Integration: verify manifest only contains allowed tools

## Files to modify
- `api/v1alpha1/mcpserver_types.go` — add fields
- `internal/mcpbridge/types.go` — add fields to ServerConfig  
- `internal/mcpbridge/bridge.go` or `client.go` — filter tools during discovery
- `internal/mcpbridge/bridge.go` — reject filtered tool calls
- `internal/controller/mcpserver_controller.go` or `internal/orchestrator/podbuilder.go` — propagate fields
- Regenerate CRD manifests (`make manifests` or `controller-gen`)
- Add unit tests

## Commit when done.
