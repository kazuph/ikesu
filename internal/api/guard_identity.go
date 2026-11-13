package api

import "encoding/json"

const MethodGuardIdentity = "guard.identity"

type GuardIdentityParams struct {
	PaneID    string `json:"pane_id"`
	PID       int    `json:"pid"`
	StartTime string `json:"start_time"`
	// 対話の zsh で、新しい行の判定が始まったとき。その行より前の、このシェルが持ち主の行の承認を消す。
	LineStart bool `json:"line_start,omitempty"`
}

type GuardIdentityResult struct {
	Type         string          `json:"type"`
	Match        bool            `json:"match"`
	Policy       json.RawMessage `json:"policy"`
	PolicyDigest string          `json:"policy_digest"`
}

type GuardIdentityContext struct {
	PaneID       string `json:"pane_id"`
	Match        bool   `json:"match"`
	PolicyDigest string `json:"policy_digest"`
	PID          int    `json:"pid"`
	StartTime    string `json:"start_time"`
}
