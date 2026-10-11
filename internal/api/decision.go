package api

const (
	MethodDecisionCreate = "decision.create"
	MethodDecisionGet    = "decision.get"
	MethodDecisionList   = "decision.list"
	MethodDecisionWait   = "decision.wait"
	MethodDecisionAnswer = "decision.answer"
	MethodDecisionCancel = "decision.cancel"
	ErrDecisionResolved  = "decision_already_resolved"
	ErrDecisionNotFound  = "decision_not_found"
)

type DecisionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Role  string `json:"role"`
}
type DecisionOrigin struct {
	PaneID  string `json:"pane_id,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Machine string `json:"machine,omitempty"`
}
type DecisionAnswer struct {
	OptionID       *string `json:"option_id"`
	Text           *string `json:"text"`
	Responder      string  `json:"responder"`
	AnsweredUnixMS int64   `json:"answered_unix_ms"`
}
type Decision struct {
	DecisionID    string           `json:"decision_id"`
	Kind          string           `json:"kind"`
	Title         string           `json:"title"`
	Body          *string          `json:"body"`
	Options       []DecisionOption `json:"options"`
	AllowText     bool             `json:"allow_text"`
	Origin        *DecisionOrigin  `json:"origin"`
	CreatedUnixMS int64            `json:"created_unix_ms"`
	ExpiresUnixMS *int64           `json:"expires_unix_ms"`
	Status        string           `json:"status"`
	Answer        *DecisionAnswer  `json:"answer"`
}
type DecisionCreateParams struct {
	Kind      string           `json:"kind"`
	Title     string           `json:"title"`
	Body      *string          `json:"body,omitempty"`
	Options   []DecisionOption `json:"options"`
	AllowText bool             `json:"allow_text"`
	Origin    *DecisionOrigin  `json:"origin,omitempty"`
	TimeoutMS *uint64          `json:"timeout_ms,omitempty"`
}
type DecisionTargetParams struct {
	DecisionID string `json:"decision_id"`
}
type DecisionListParams struct {
	Status string `json:"status,omitempty"`
}
type DecisionWaitParams struct {
	DecisionID string  `json:"decision_id"`
	TimeoutMS  *uint64 `json:"timeout_ms,omitempty"`
}
type DecisionAnswerParams struct {
	DecisionID string  `json:"decision_id"`
	OptionID   *string `json:"option_id,omitempty"`
	Text       *string `json:"text,omitempty"`
	Responder  string  `json:"responder"`
}
type DecisionResult struct {
	Type     string   `json:"type"`
	Decision Decision `json:"decision"`
}
type DecisionListResult struct {
	Type      string     `json:"type"`
	Decisions []Decision `json:"decisions"`
}
