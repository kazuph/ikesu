package api

// AI-link parameter and result types (msg.*, run.*, agent.*). Field names
// mirror docs/ref/herdr-api-schema.json.

type MsgSendParams struct {
	Room      string `json:"room"`
	Project   string `json:"project"`
	FromAgent string `json:"from_agent"`
	To        string `json:"to"`
	Body      string `json:"body"`
	ReplyTo   *int64 `json:"reply_to"`
}

type MsgInboxParams struct {
	Room    string `json:"room"`
	ToAgent string `json:"to_agent"`
}

type MsgHistoryParams struct {
	Room    string  `json:"room"`
	Project *string `json:"project"`
	Limit   uint32  `json:"limit"`
}

// MsgMessage is one mailbox row on the wire.
type MsgMessage struct {
	ID          int64   `json:"id"`
	Room        string  `json:"room"`
	Project     string  `json:"project"`
	FromAgent   string  `json:"from_agent"`
	ToAgent     string  `json:"to_agent"`
	Body        string  `json:"body"`
	CreatedAt   string  `json:"created_at"`
	DeliveredAt *string `json:"delivered_at"`
	ReadAt      *string `json:"read_at"`
}

type MsgSendResult struct {
	Type     string       `json:"type"` // "msg_send"
	Messages []MsgMessage `json:"messages"`
	Nudged   []string     `json:"nudged"`
}

type MsgInboxResult struct {
	Type     string       `json:"type"` // "msg_inbox"
	Messages []MsgMessage `json:"messages"`
}

type MsgHistoryResult struct {
	Type     string       `json:"type"` // "msg_history"
	Messages []MsgMessage `json:"messages"`
}

type MsgRoomsResult struct {
	Type  string   `json:"type"` // "msg_rooms"
	Rooms []string `json:"rooms"`
}

// --- run ---

type RunStartParams struct {
	CallerPane string   `json:"caller_pane"`
	Label      string   `json:"label"`
	Cwd        string   `json:"cwd"`
	Argv       []string `json:"argv"`
	Completion string   `json:"completion"`
}

type RunStartedResult struct {
	Type  string `json:"type"` // "run_started"
	Job   string `json:"job"`
	Label string `json:"label"`
	Mode  string `json:"mode"` // "background"
}

type RunCancelParams struct {
	JobID string `json:"job_id"`
}

type RunCancelledResult struct {
	Type   string `json:"type"` // "run_cancelled"
	Job    string `json:"job"`
	Status string `json:"status"` // "cancelled"
	Signal string `json:"signal"` // "TERM" | "KILL"
}

// --- agent ---

type AgentTarget struct {
	Target string `json:"target"`
}

type AgentReadParams struct {
	Target    string  `json:"target"`
	Source    string  `json:"source"`
	Lines     *uint32 `json:"lines,omitempty"`
	Format    string  `json:"format,omitempty"`
	StripANSI bool    `json:"strip_ansi"`
}

type AgentSendParams struct {
	Target string `json:"target"`
	Text   string `json:"text"`
}

type AgentRenameParams struct {
	Target string  `json:"target"`
	Name   *string `json:"name"`
}

type AgentStartParams struct {
	Name        string            `json:"name"`
	Argv        []string          `json:"argv"`
	Cwd         *string           `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkspaceID *string           `json:"workspace_id,omitempty"`
	TabID       *string           `json:"tab_id,omitempty"`
	Split       *string           `json:"split,omitempty"`
	Focus       bool              `json:"focus"`
}

// AgentInfo mirrors herdr's AgentInfo where ikesu has an equivalent field.
// global_pane_id and pane_id are the same public pN id in ikesu.
type AgentInfo struct {
	Agent          *string           `json:"agent"`
	AgentSession   *AgentSessionInfo `json:"agent_session"`
	AgentStatus    AgentStatus       `json:"agent_status"`
	Cwd            *string           `json:"cwd"`
	DisplayAgent   *string           `json:"display_agent"`
	Focused        bool              `json:"focused"`
	ForegroundCwd  *string           `json:"foreground_cwd"`
	GlobalPaneID   string            `json:"global_pane_id"`
	GlobalPaneNum  int               `json:"global_pane_number"`
	InteractiveRdy bool              `json:"interactive_ready"`
	LaunchPending  bool              `json:"launch_pending"`
	Name           *string           `json:"name"`
	PaneID         string            `json:"pane_id"`
	Revision       uint64            `json:"revision"`
	ShortPaneID    string            `json:"short_pane_id"`
	StateChangeSeq uint64            `json:"state_change_seq"`
	StateLabels    map[string]string `json:"state_labels,omitempty"`
	TabID          string            `json:"tab_id"`
	TerminalID     string            `json:"terminal_id"`
	TerminalTitle  *string           `json:"terminal_title"`
	Title          *string           `json:"title"`
	WorkspaceID    string            `json:"workspace_id"`
}

type AgentListResult struct {
	Type   string      `json:"type"` // "agent_list"
	Agents []AgentInfo `json:"agents"`
}

type AgentInfoResult struct {
	Type  string    `json:"type"` // "agent_info"
	Agent AgentInfo `json:"agent"`
}

type AgentStartedResult struct {
	Argv  []string  `json:"argv"`
	Type  string    `json:"type"` // "agent_started"
	Agent AgentInfo `json:"agent"`
}

type AgentReadResultEnvelope struct {
	Type string         `json:"type"` // "agent_read"
	Read PaneReadResult `json:"read"`
}
