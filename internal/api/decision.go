package api

const (
	MethodDecisionCreate        = "decision.create"
	MethodDecisionGuardApproved = "decision.guard-approved"
	MethodDecisionGet           = "decision.get"
	MethodDecisionList          = "decision.list"
	MethodDecisionWait          = "decision.wait"
	MethodDecisionAnswer        = "decision.answer"
	MethodDecisionCancel        = "decision.cancel"
	MethodDecisionReach         = "decision.reach"
	ErrDecisionResolved         = "decision_already_resolved"
	ErrDecisionNotFound         = "decision_not_found"
	ErrDecisionStale            = "decision_stale"
)

type DecisionOption struct {
	ID                 string  `json:"id"`
	Label              string  `json:"label"`
	Role               string  `json:"role"`
	Description        *string `json:"description,omitempty"`
	Preview            *string `json:"preview,omitempty"`
	PreviewUnavailable bool    `json:"preview_unavailable,omitempty"`
}
type DecisionOrigin struct {
	OutsidePane    bool   `json:"outside_pane,omitempty"`
	WorkspaceLabel string `json:"workspace_label,omitempty"`
	TabLabel       string `json:"tab_label,omitempty"`
	PaneLabel      string `json:"pane_label,omitempty"`
	PaneID         string `json:"pane_id,omitempty"`
	Agent          string `json:"agent,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Command        string `json:"command,omitempty"`
	Tool           string `json:"tool,omitempty"`
	Explanation    string `json:"explanation,omitempty"`
	Warning        string `json:"warning,omitempty"`
	ScreenColumns  int    `json:"screen_columns,omitempty"`
	TargetCommand  string `json:"target_command,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Machine        string `json:"machine,omitempty"`
	// GuardRule・GuardReason は guard が ask にした規則と判定理由（申請文の Reason とは別）。
	GuardRule   string `json:"guard_rule,omitempty"`
	GuardReason string `json:"guard_reason,omitempty"`
	// CommandUnits は表示用コマンド（Command）を最上位の && || ; | 改行で区切った行ごとの判定。
	// 無いときは今までどおり Command と TargetCommand だけで表示する。
	CommandUnits []CommandUnit `json:"command_units,omitempty"`
}

// CommandUnit はダイアログの1行。Text は伏せ字済みの Command から切り出した文字列。
type CommandUnit struct {
	Op      string `json:"op"`               // 前の行とのつなぎ: "" "&&" "||" "|" ";" "\n"
	Text    string `json:"text"`             // その行のコマンド（伏せ字済み）
	Label   string `json:"label"`            // 移動・読む・書く・消す・実行・外へ送る・別マシン・不明
	Verdict string `json:"verdict"`          // allow・ask・block・unjudged（block の後ろで判定していない）
	Rule    string `json:"rule,omitempty"`   // ask・block の規則
	Reason  string `json:"reason,omitempty"` // ask・block の判定理由（伏せ字済み）
	Target  string `json:"target,omitempty"` // スクリプトの中などで引っかかった命令（伏せ字済み）
}
type DecisionAnswer struct {
	OptionID       *string  `json:"option_id"`
	OptionIDs      []string `json:"option_ids,omitempty"`
	Text           *string  `json:"text"`
	Responder      string   `json:"responder"`
	AnsweredUnixMS int64    `json:"answered_unix_ms"`
}
type DecisionDelivery struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	Reason string `json:"reason,omitempty"`
}
type Decision struct {
	DecisionID    string            `json:"decision_id"`
	Kind          string            `json:"kind"`
	Title         string            `json:"title"`
	Body          *string           `json:"body"`
	Options       []DecisionOption  `json:"options"`
	AllowText     bool              `json:"allow_text"`
	MultiSelect   bool              `json:"multi_select"`
	QuestionLabel *string           `json:"question_label,omitempty"`
	Origin        *DecisionOrigin   `json:"origin"`
	CreatedUnixMS int64             `json:"created_unix_ms"`
	ExpiresUnixMS *int64            `json:"expires_unix_ms"`
	Status        string            `json:"status"`
	Answer        *DecisionAnswer   `json:"answer"`
	Delivery      *DecisionDelivery `json:"delivery,omitempty"`
	LateAnswer    bool              `json:"late_answer,omitempty"`
	// Reach は送り先ごとの「届いた」記録（channel は mac-panel・android・slack）。
	Reach map[string]DecisionReach `json:"reach,omitempty"`
}

// DecisionReach は1つの送り先の到達状態。State は "shown"（通知を出した）か
// "no_client"（送り先の端末がつながっていない）。AtUnixMS は最初に記録した時刻。
type DecisionReach struct {
	State    string `json:"state"`
	AtUnixMS int64  `json:"at_unix_ms"`
}

// ReachChannelIDs は decision.reach と [notify] の channel に使える id。
var ReachChannelIDs = map[string]bool{"mac-panel": true, "android": true, "slack": true}

const (
	ReachShown    = "shown"
	ReachNoClient = "no_client"
)

type DecisionReachParams struct {
	DecisionID string `json:"decision_id"`
	Channel    string `json:"channel"`
	State      string `json:"state"`
}

// GuardLineApprovedRule は、行の承認が通す claim の規則名。
const GuardLineApprovedRule = "line-approved"

// GuardLineApprovalLimit は、1つの判断待ちに付けられる行の claim の上限。
const GuardLineApprovalLimit = 64

type GuardExecutionClaim struct {
	CommandHash string `json:"command_hash"`
	Rule        string `json:"rule"`
}
type GuardApprovedParams struct {
	GuardExecutionClaim
	PaneID string `json:"pane_id"`
}
type GuardApprovedResult struct {
	Approved bool `json:"approved"`
}

type DecisionCreateParams struct {
	GuardContext   *GuardIdentityContext `json:"guard_context,omitempty"`
	GuardExecution *GuardExecutionClaim  `json:"guard_execution,omitempty"`
	// 行の承認が通す、行の中の ask のコマンドごとの claim（規則名は GuardLineApprovedRule）。
	GuardExecutions []GuardExecutionClaim `json:"guard_executions,omitempty"`
	Kind            string                `json:"kind"`
	Title           string                `json:"title"`
	Body            *string               `json:"body,omitempty"`
	Options         []DecisionOption      `json:"options"`
	AllowText       bool                  `json:"allow_text"`
	MultiSelect     bool                  `json:"multi_select"`
	QuestionLabel   *string               `json:"-"`
	Origin          *DecisionOrigin       `json:"origin,omitempty"`
	TimeoutMS       *uint64               `json:"timeout_ms,omitempty"`
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
	DecisionID string   `json:"decision_id"`
	OptionID   *string  `json:"option_id,omitempty"`
	OptionIDs  []string `json:"option_ids,omitempty"`
	Text       *string  `json:"text,omitempty"`
	Responder  string   `json:"responder"`
}
type DecisionResult struct {
	Type     string   `json:"type"`
	Decision Decision `json:"decision"`
}
type DecisionListResult struct {
	Type      string     `json:"type"`
	Decisions []Decision `json:"decisions"`
}
