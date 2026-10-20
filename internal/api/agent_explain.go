package api

import "github.com/kazuph/ikesu/internal/detect"

const MethodAgentExplain = "agent.explain"

type AgentExplainResult struct {
	Type    string             `json:"type"`
	Explain detect.Explanation `json:"explain"`
}
