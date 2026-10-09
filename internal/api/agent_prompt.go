package api

// DefaultAgentWaitTimeoutMS is the existing agent-wait CLI default.
const DefaultAgentWaitTimeoutMS = 30000

type AgentPromptParams struct {
	Target string                  `json:"target"`
	Text   string                  `json:"text"`
	Wait   *AgentPromptWaitOptions `json:"wait,omitempty"`
}

type AgentPromptWaitOptions struct {
	TimeoutMS *uint64       `json:"timeout_ms,omitempty"`
	Until     []AgentStatus `json:"until"`
}

type AgentPromptedResult struct {
	Type  string    `json:"type"`
	Agent AgentInfo `json:"agent"`
}
