package api

import "github.com/kazuph/goterrace/internal/detect"

const MethodAgentExplain = "agent.explain"

type AgentExplainResult struct {
	Type    string             `json:"type"`
	Explain detect.Explanation `json:"explain"`
}
