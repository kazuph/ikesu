package api

// Parameter types for the implemented method subset. Field names and types
// mirror docs/ref/herdr-api-schema.json.

type EmptyParams struct{}

type WorkspaceCreateParams struct {
	Cwd   *string           `json:"cwd,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Focus bool              `json:"focus"`
	Label *string           `json:"label,omitempty"`
}

type WorkspaceTarget struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceRenameParams struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type TabCreateParams struct {
	WorkspaceID *string           `json:"workspace_id,omitempty"`
	Cwd         *string           `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Focus       bool              `json:"focus"`
	Label       *string           `json:"label,omitempty"`
}

type TabTarget struct {
	TabID string `json:"tab_id"`
}

type TabListParams struct {
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type TabRenameParams struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

type PaneListParams struct {
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type PaneCurrentParams struct {
	CallerPaneID    *string `json:"caller_pane_id,omitempty"`
	CallerProcessID *uint32 `json:"caller_process_id,omitempty"`
}

type PaneTarget struct {
	PaneID string `json:"pane_id"`
}

type PaneFocusParams struct {
	PaneID string `json:"pane_id"`
	Viewer any    `json:"viewer,omitempty"`
}

type PaneRenameParams struct {
	PaneID string  `json:"pane_id"`
	Label  *string `json:"label"`
}

type PaneReadParams struct {
	PaneID    string  `json:"pane_id"`
	Source    string  `json:"source"`
	Lines     *uint32 `json:"lines,omitempty"`
	Format    string  `json:"format,omitempty"`
	StripANSI bool    `json:"strip_ansi"`
}

type PaneSplitParams struct {
	TargetPaneID *string           `json:"target_pane_id,omitempty"`
	WorkspaceID  *string           `json:"workspace_id,omitempty"`
	Direction    string            `json:"direction"`
	Ratio        *float64          `json:"ratio,omitempty"`
	Cwd          *string           `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Focus        bool              `json:"focus"`
}

type PaneQueryParams struct {
	PaneID string `json:"pane_id"`
}

type PaneSendTextParams struct {
	PaneID string `json:"pane_id"`
	Text   string `json:"text"`
}

type PaneSendKeysParams struct {
	PaneID string   `json:"pane_id"`
	Keys   []string `json:"keys"`
}

type PaneSendInputParams struct {
	PaneID string   `json:"pane_id"`
	Text   string   `json:"text"`
	Keys   []string `json:"keys"`
}

type PaneReportAgentParams struct {
	PaneID           string  `json:"pane_id"`
	Source           string  `json:"source"`
	Agent            string  `json:"agent"`
	State            string  `json:"state"`
	Message          *string `json:"message,omitempty"`
	CustomStatus     *string `json:"custom_status,omitempty"`
	Seq              *uint64 `json:"seq,omitempty"`
	Title            *string `json:"title,omitempty"`
	SessionID        *string `json:"session_id,omitempty"`
	AgentSessionID   *string `json:"agent_session_id,omitempty"`
	AgentSessionPath *string `json:"agent_session_path,omitempty"`
	Model            *string `json:"model,omitempty"`
}

// Result types. Each envelope carries a "type" discriminator first, matching
// the herdr ResponseResult wire shape.

type PongResult struct {
	Type     string `json:"type"` // "pong"
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

type OkResult struct {
	Type string `json:"type"` // "ok"
}

type AgentStatus = string

const (
	AgentIdle    AgentStatus = "idle"
	AgentWorking AgentStatus = "working"
	AgentBlocked AgentStatus = "blocked"
	AgentDone    AgentStatus = "done"
	AgentUnknown AgentStatus = "unknown"
)

// WorkspaceInfo fields are emitted in alphabetical order to match herdr.
type WorkspaceInfo struct {
	ActiveTabID         string      `json:"active_tab_id"`
	AgentStatus         AgentStatus `json:"agent_status"`
	Focused             bool        `json:"focused"`
	Label               string      `json:"label"`
	NextPublicTabNumber *int        `json:"next_public_tab_number,omitempty"`
	Number              int         `json:"number"`
	PaneCount           int         `json:"pane_count"`
	TabCount            int         `json:"tab_count"`
	WorkspaceID         string      `json:"workspace_id"`
}

type WorkspaceListResult struct {
	Type       string          `json:"type"` // "workspace_list"
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

type WorkspaceInfoResult struct {
	Type      string        `json:"type"` // "workspace_info"
	Workspace WorkspaceInfo `json:"workspace"`
}

type WorkspaceCreatedResult struct {
	Type      string        `json:"type"` // "workspace_created"
	Workspace WorkspaceInfo `json:"workspace"`
	Tab       TabInfo       `json:"tab"`
	RootPane  PaneInfo      `json:"root_pane"`
}

// TabInfo fields are emitted in alphabetical order to match herdr.
type TabInfo struct {
	AgentStatus AgentStatus `json:"agent_status"`
	Focused     bool        `json:"focused"`
	Label       string      `json:"label"`
	Number      int         `json:"number"`
	PaneCount   int         `json:"pane_count"`
	TabID       string      `json:"tab_id"`
	WorkspaceID string      `json:"workspace_id"`
}

type TabListResult struct {
	Type string    `json:"type"` // "tab_list"
	Tabs []TabInfo `json:"tabs"`
}

type TabInfoResult struct {
	Type string  `json:"type"` // "tab_info"
	Tab  TabInfo `json:"tab"`
}

type TabCreatedResult struct {
	Type     string   `json:"type"` // "tab_created"
	Tab      TabInfo  `json:"tab"`
	RootPane PaneInfo `json:"root_pane"`
}

type AgentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"` // "id" | "path"
	Value  string `json:"value"`
}

type PaneScrollInfo struct {
	OffsetFromBottom    int `json:"offset_from_bottom"`
	MaxOffsetFromBottom int `json:"max_offset_from_bottom"`
	ViewportRows        int `json:"viewport_rows"`
}

// PaneInfo fields are emitted in alphabetical order to match herdr's output.
type PaneInfo struct {
	Agent                 *string           `json:"agent,omitempty"`
	AgentSession          *AgentSessionInfo `json:"agent_session,omitempty"`
	AgentStatus           AgentStatus       `json:"agent_status"`
	Cwd                   *string           `json:"cwd,omitempty"`
	DisplayAgent          *string           `json:"display_agent,omitempty"`
	Focused               bool              `json:"focused"`
	ForegroundCwd         *string           `json:"foreground_cwd,omitempty"`
	Label                 *string           `json:"label,omitempty"`
	PaneID                string            `json:"pane_id"`
	Revision              uint64            `json:"revision"`
	Scroll                *PaneScrollInfo   `json:"scroll"`
	StateLabels           map[string]string `json:"state_labels,omitempty"`
	TabID                 string            `json:"tab_id"`
	TerminalID            string            `json:"terminal_id"`
	TerminalTitle         *string           `json:"terminal_title,omitempty"`
	TerminalTitleStripped *string           `json:"terminal_title_stripped,omitempty"`
	Title                 *string           `json:"title,omitempty"`
	Tokens                map[string]string `json:"tokens,omitempty"`
	WorkspaceID           string            `json:"workspace_id"`
}

type PaneListResult struct {
	Type  string     `json:"type"` // "pane_list"
	Panes []PaneInfo `json:"panes"`
}

type PaneInfoResult struct {
	Type string   `json:"type"` // "pane_info"
	Pane PaneInfo `json:"pane"`
}

type PaneCurrentResult struct {
	Type string   `json:"type"` // "pane_current"
	Pane PaneInfo `json:"pane"`
}

type PaneReadResult struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Source      string `json:"source"`
	Format      string `json:"format"`
	Text        string `json:"text"`
	Revision    uint64 `json:"revision"`
	Truncated   bool   `json:"truncated"`
}

type PaneReadResultEnvelope struct {
	Type string         `json:"type"` // "pane_read"
	Read PaneReadResult `json:"read"`
}
