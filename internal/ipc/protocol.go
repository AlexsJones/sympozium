package ipc

import "encoding/json"

// Protocol types for IPC file-based communication.

// TaskInput is written to /ipc/input/task.json by the orchestrator.
type TaskInput struct {
	Task         string          `json:"task"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	AgentID      string          `json:"agentId"`
	SessionKey   string          `json:"sessionKey"`
	Model        ModelConfig     `json:"model"`
	Tools        []string        `json:"tools,omitempty"`
	Context      json.RawMessage `json:"context,omitempty"`
}

// ModelConfig specifies the LLM configuration.
type ModelConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Thinking string `json:"thinking,omitempty"`
}

// AgentResult is written to /ipc/output/result.json by the agent on completion.
type AgentResult struct {
	Status   string `json:"status"` // "success" or "error"
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
	Metrics  struct {
		DurationMs     int64 `json:"durationMs"`
		InputTokens    int   `json:"inputTokens"`
		OutputTokens   int   `json:"outputTokens"`
		ToolCalls      int   `json:"toolCalls"`
		SubagentSpawns int   `json:"subagentSpawns"`
	} `json:"metrics"`
}

// StreamChunk is written to /ipc/output/stream-*.json for streaming responses.
type StreamChunk struct {
	Type    string `json:"type"` // "text", "thinking", "tool_use", "tool_result"
	Content string `json:"content"`
	ToolID  string `json:"toolId,omitempty"`
	Index   int    `json:"index"`
}

// SpawnRequest is written to /ipc/spawn/request-*.json to request sub-agent creation.
type SpawnRequest struct {
	Task         string   `json:"task"`
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	AgentID      string   `json:"agentId"`
	Skills       []string `json:"skills,omitempty"`
}

// ExecRequest is written to /ipc/tools/exec-request-*.json for sandbox execution.
type ExecRequest struct {
	ID      string   `json:"id"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	WorkDir string   `json:"workDir,omitempty"`
	Timeout int      `json:"timeout,omitempty"` // seconds
	Stdin   string   `json:"stdin,omitempty"`
}

// ExecResult is written to /ipc/tools/exec-result-*.json with execution results.
type ExecResult struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	TimedOut bool   `json:"timedOut,omitempty"`
}

// OutboundMessage is written to /ipc/messages/send-*.json for channel delivery.
// Field names align with channel.OutboundMessage so the bridge can relay the
// JSON directly without remapping.
type OutboundMessage struct {
	Channel  string          `json:"channel"`          // "telegram", "whatsapp", etc.
	ChatID   string          `json:"chatId,omitempty"` // Chat/group ID; empty = owner/self
	Text     string          `json:"text"`
	Format   string          `json:"format,omitempty"` // "plain", "markdown", "html"
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// StatusUpdate is written to /ipc/output/status.json for agent status.
type StatusUpdate struct {
	Phase   string `json:"phase"` // "thinking", "tool_use", "responding"
	Message string `json:"message,omitempty"`
	ToolID  string `json:"toolId,omitempty"`
}
