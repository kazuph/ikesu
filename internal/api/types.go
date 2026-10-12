package api

import "encoding/json"

// Parameter types for the implemented method subset. Field names and types
// mirror docs/ref/herdr-api-schema.json.

type EmptyParams struct{}

type WorkspaceCreateParams struct {
	Section *string           `json:"section,omitempty"`
	Cwd     *string           `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Focus   bool              `json:"focus"`
	Label   *string           `json:"label,omitempty"`
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

type ServerLiveHandoffParams struct {
	ExpectedProtocol *uint32 `json:"expected_protocol,omitempty"`
	ExpectedVersion  *string `json:"expected_version,omitempty"`
	ImportExe        *string `json:"import_exe,omitempty"`
}

type PongResult struct {
	Capabilities map[string]any `json:"capabilities,omitempty"`
	Type         string         `json:"type"` // "pong"
	Version      string         `json:"version"`
	Protocol     int            `json:"protocol"`
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
	Tokens              map[string]string      `json:"tokens,omitempty"`
	Worktree            *WorkspaceWorktreeInfo `json:"worktree,omitempty"`
	ActiveTabID         string                 `json:"active_tab_id"`
	AgentStatus         AgentStatus            `json:"agent_status"`
	Focused             bool                   `json:"focused"`
	Label               string                 `json:"label"`
	NextPublicTabNumber *int                   `json:"next_public_tab_number,omitempty"`
	Number              int                    `json:"number"`
	PaneCount           int                    `json:"pane_count"`
	TabCount            int                    `json:"tab_count"`
	WorkspaceID         string                 `json:"workspace_id"`
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
	SessionStartSource string `json:"session_start_source,omitempty"`
	Source             string `json:"source"`
	Agent              string `json:"agent"`
	Kind               string `json:"kind"` // "id" | "path"
	Value              string `json:"value"`
}

// AgentRestoreParams drives `agent.restore`; dry-run plans without typing.
type AgentRestoreParams struct {
	DryRun bool `json:"dry_run,omitempty"`
}

// AgentRestoreActionInfo is the per-pane outcome of an agent.restore call
// (SPEC G3). Status is launched / would_launch / skipped.
type AgentRestoreActionInfo struct {
	PaneID  string  `json:"pane_id"`
	Agent   string  `json:"agent"`
	Status  string  `json:"status"`
	Command *string `json:"command"`
	Reason  *string `json:"reason"`
}

// AgentSessionWarningInfo is one entry of the missing-session warning
// list shown before restart (SPEC G3 fail-closed restart warning).
type AgentSessionWarningInfo struct {
	WorkspaceNumber int     `json:"workspace_number"`
	WorkspaceLabel  string  `json:"workspace_label"`
	PaneLabel       string  `json:"pane_label"`
	Agent           string  `json:"agent"`
	Cwd             string  `json:"cwd"`
	Reason          string  `json:"reason"`
	Title           *string `json:"title"`
}

// AgentRestoreResult is the success payload of `agent.restore`.
type AgentRestoreResult struct {
	Type    string                   `json:"type"` // "agent_restore"
	DryRun  bool                     `json:"dry_run"`
	Actions []AgentRestoreActionInfo `json:"actions"`
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

// --- G1 layout / pane-move API surface ---

type PaneLayoutRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type PaneLayoutPane struct {
	PaneID  string         `json:"pane_id"`
	Focused bool           `json:"focused"`
	Rect    PaneLayoutRect `json:"rect"`
}

type PaneLayoutSplit struct {
	ID        string         `json:"id"`
	Direction string         `json:"direction"` // "right" | "down"
	Ratio     float64        `json:"ratio"`
	Rect      PaneLayoutRect `json:"rect"`
}

type PaneLayoutSnapshot struct {
	WorkspaceID   string            `json:"workspace_id"`
	TabID         string            `json:"tab_id"`
	Zoomed        bool              `json:"zoomed"`
	Area          PaneLayoutRect    `json:"area"`
	FocusedPaneID string            `json:"focused_pane_id"`
	Panes         []PaneLayoutPane  `json:"panes"`
	Splits        []PaneLayoutSplit `json:"splits"`
}

// LayoutNode is the exported/applied tree node (layout.export/apply).
type LayoutNode struct {
	Type      string            `json:"type"` // "pane" | "split"
	Direction string            `json:"direction,omitempty"`
	Ratio     float64           `json:"ratio,omitempty"`
	First     *LayoutNode       `json:"first,omitempty"`
	Second    *LayoutNode       `json:"second,omitempty"`
	PaneID    *string           `json:"pane_id,omitempty"`
	Cwd       *string           `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Label     *string           `json:"label,omitempty"`
	Command   []string          `json:"command,omitempty"`
}

type LayoutDescription struct {
	WorkspaceID   string      `json:"workspace_id"`
	TabID         string      `json:"tab_id"`
	Zoomed        bool        `json:"zoomed"`
	FocusedPaneID string      `json:"focused_pane_id"`
	Root          *LayoutNode `json:"root"`
}

type PaneLayoutParams struct {
	PaneID *string `json:"pane_id,omitempty"`
	TabID  *string `json:"tab_id,omitempty"`
}

type PaneNeighborParams struct {
	PaneID    *string `json:"pane_id,omitempty"`
	Direction string  `json:"direction"`
}

type PaneEdgesParams struct {
	PaneID *string `json:"pane_id,omitempty"`
}

type PaneFocusDirectionParams struct {
	PaneID    *string `json:"pane_id,omitempty"`
	Direction string  `json:"direction"`
}

type PaneResizeParams struct {
	PaneID    *string  `json:"pane_id,omitempty"`
	Direction string   `json:"direction"`
	Amount    *float64 `json:"amount,omitempty"`
}

type PaneSwapParams struct {
	PaneID       *string `json:"pane_id,omitempty"`
	Direction    *string `json:"direction,omitempty"`
	SourcePaneID *string `json:"source_pane_id,omitempty"`
	TargetPaneID *string `json:"target_pane_id,omitempty"`
}

type PaneZoomParams struct {
	PaneID *string `json:"pane_id,omitempty"`
	Mode   string  `json:"mode,omitempty"`
}

type PaneArrangeParams struct {
	PaneID      string `json:"pane_id"`
	Arrangement string `json:"arrangement"`
}

// PaneMoveDestination mirrors the oneOf in the herdr schema. Exactly one
// variant is populated per request.
type PaneMoveDestination struct {
	Type         string   `json:"type"` // "tab" | "new_tab" | "new_workspace"
	TabID        string   `json:"tab_id,omitempty"`
	Split        *string  `json:"split,omitempty"`
	TargetPaneID *string  `json:"target_pane_id,omitempty"`
	Ratio        *float64 `json:"ratio,omitempty"`
	WorkspaceID  *string  `json:"workspace_id,omitempty"`
	Label        *string  `json:"label,omitempty"`
	TabLabel     *string  `json:"tab_label,omitempty"`
}

type PaneMoveParams struct {
	PaneID      string              `json:"pane_id"`
	Destination PaneMoveDestination `json:"destination"`
	Focus       bool                `json:"focus"`
}

type LayoutExportParams struct {
	PaneID *string `json:"pane_id,omitempty"`
	TabID  *string `json:"tab_id,omitempty"`
}

type LayoutApplyParams struct {
	WorkspaceID *string     `json:"workspace_id,omitempty"`
	TabID       *string     `json:"tab_id,omitempty"`
	TabLabel    *string     `json:"tab_label,omitempty"`
	Focus       bool        `json:"focus"`
	Root        *LayoutNode `json:"root"`
}

type LayoutSetSplitRatioParams struct {
	TabID  *string `json:"tab_id,omitempty"`
	PaneID *string `json:"pane_id,omitempty"`
	Path   []bool  `json:"path"`
	Ratio  float64 `json:"ratio"`
}

type PaneScrollParams struct {
	PaneID           string `json:"pane_id"`
	OffsetFromBottom uint64 `json:"offset_from_bottom"`
}

// --- copy buffer contract (pane.copy_motion / pane.copy_search) ---

// PaneTextPoint is an absolute retained-buffer coordinate.
type PaneTextPoint struct {
	Row uint32 `json:"row"`
	Col uint16 `json:"col"`
}

type PaneTextRange struct {
	Start PaneTextPoint `json:"start"`
	End   PaneTextPoint `json:"end"`
}

type PaneCopyMotionParams struct {
	PaneID          string        `json:"pane_id"`
	Cursor          PaneTextPoint `json:"cursor"`
	Motion          string        `json:"motion"`
	ContentRevision *uint64       `json:"content_revision,omitempty"`
}

type PaneCopyMotionResult struct {
	Type            string        `json:"type"` // "pane_copy_motion"
	PaneID          string        `json:"pane_id"`
	Cursor          PaneTextPoint `json:"cursor"`
	ContentRevision uint64        `json:"content_revision"`
}

type PaneCopySearchParams struct {
	PaneID          string         `json:"pane_id"`
	Query           string         `json:"query"`
	Direction       string         `json:"direction"` // "forward" | "backward"
	Cursor          PaneTextPoint  `json:"cursor"`
	ContentRevision uint64         `json:"content_revision"`
	Previous        *PaneTextRange `json:"previous,omitempty"`
}

type PaneCopySearchResult struct {
	Type            string          `json:"type"` // "pane_copy_search"
	PaneID          string          `json:"pane_id"`
	ContentRevision uint64          `json:"content_revision"`
	Matches         []PaneTextRange `json:"matches"`
	Total           uint64          `json:"total"`
	Current         *uint32         `json:"current,omitempty"`
	CurrentGlobal   *uint64         `json:"current_global,omitempty"`
}

// --- events ---

type Subscription struct {
	Type string `json:"type"` // "pane.focused" etc.
}

type EventsSubscribeParams struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// EventMatch is a generic map so every variant in the schema can be
// decoded; the server validates the supported subset.
type EventMatch map[string]any

type EventsWaitParams struct {
	MatchEvent EventMatch `json:"match_event"`
	TimeoutMs  *uint64    `json:"timeout_ms,omitempty"`
}

// EventEnvelope is pushed to event subscribers after subscription_started.
type EventEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type SubscriptionStartedResult struct {
	Type string `json:"type"` // "subscription_started"
}

type WaitMatchedResult struct {
	Type  string         `json:"type"` // "wait_matched"
	Event *EventEnvelope `json:"event"`
}

// --- layout op results ---
// Every op result nests its payload under an op-named key inside a
// type-tagged envelope (verified against the released binary's wire).

type PaneLayoutResult struct {
	Type   string             `json:"type"` // "pane_layout"
	Layout PaneLayoutSnapshot `json:"layout"`
}

type PaneNeighborEnvelope struct {
	Type     string             `json:"type"` // "pane_neighbor"
	Neighbor PaneNeighborResult `json:"neighbor"`
}

type PaneNeighborResult struct {
	PaneID         string             `json:"pane_id"`
	Direction      string             `json:"direction"`
	NeighborPaneID *string            `json:"neighbor_pane_id,omitempty"`
	Layout         PaneLayoutSnapshot `json:"layout"`
}

type PaneEdgesEnvelope struct {
	Type  string          `json:"type"` // "pane_edges"
	Edges PaneEdgesResult `json:"edges"`
}

type PaneEdgesResult struct {
	PaneID string             `json:"pane_id"`
	Left   bool               `json:"left"`
	Right  bool               `json:"right"`
	Up     bool               `json:"up"`
	Down   bool               `json:"down"`
	Layout PaneLayoutSnapshot `json:"layout"`
}

type PaneFocusDirectionEnvelope struct {
	Type  string                   `json:"type"` // "pane_focus_direction"
	Focus PaneFocusDirectionResult `json:"focus"`
}

type PaneFocusDirectionResult struct {
	Changed       bool               `json:"changed"`
	Reason        *string            `json:"reason,omitempty"`
	SourcePaneID  string             `json:"source_pane_id"`
	FocusedPaneID string             `json:"focused_pane_id"`
	Layout        PaneLayoutSnapshot `json:"layout"`
}

type PaneResizeEnvelope struct {
	Type   string           `json:"type"` // "pane_resize"
	Resize PaneResizeResult `json:"resize"`
}

type PaneResizeResult struct {
	Changed       bool               `json:"changed"`
	Reason        *string            `json:"reason,omitempty"`
	PaneID        string             `json:"pane_id"`
	FocusedPaneID string             `json:"focused_pane_id"`
	Layout        PaneLayoutSnapshot `json:"layout"`
}

type PaneSwapEnvelope struct {
	Type string         `json:"type"` // "pane_swap"
	Swap PaneSwapResult `json:"swap"`
}

type PaneSwapResult struct {
	Changed       bool               `json:"changed"`
	Reason        *string            `json:"reason,omitempty"`
	SourcePaneID  string             `json:"source_pane_id"`
	TargetPaneID  *string            `json:"target_pane_id,omitempty"`
	FocusedPaneID string             `json:"focused_pane_id"`
	Layout        PaneLayoutSnapshot `json:"layout"`
}

type PaneZoomEnvelope struct {
	Type string         `json:"type"` // "pane_zoom"
	Zoom PaneZoomResult `json:"zoom"`
}

type PaneZoomResult struct {
	Changed       bool               `json:"changed"`
	ZoomChanged   bool               `json:"zoom_changed"`
	FocusChanged  bool               `json:"focus_changed"`
	Reason        *string            `json:"reason,omitempty"`
	PaneID        string             `json:"pane_id"`
	FocusedPaneID string             `json:"focused_pane_id"`
	Zoomed        bool               `json:"zoomed"`
	Layout        PaneLayoutSnapshot `json:"layout"`
}

type PaneMoveEnvelope struct {
	Type       string         `json:"type"` // "pane_move"
	MoveResult PaneMoveResult `json:"move_result"`
}

type PaneMoveResult struct {
	Changed             bool                `json:"changed"`
	PreviousPaneID      string              `json:"previous_pane_id"`
	PreviousWorkspaceID string              `json:"previous_workspace_id"`
	PreviousTabID       string              `json:"previous_tab_id"`
	Pane                PaneInfo            `json:"pane"`
	SourceLayout        *PaneLayoutSnapshot `json:"source_layout"`
	TargetLayout        *PaneLayoutSnapshot `json:"target_layout"`
	FocusedPaneID       string              `json:"focused_pane_id"`
	CreatedTab          *TabInfo            `json:"created_tab,omitempty"`
	CreatedWorkspace    *WorkspaceInfo      `json:"created_workspace,omitempty"`
	ClosedTabID         *string             `json:"closed_tab_id,omitempty"`
	ClosedWorkspaceID   *string             `json:"closed_workspace_id,omitempty"`
	Reason              *string             `json:"reason,omitempty"`
}

type LayoutExportResult struct {
	Type   string            `json:"type"` // "layout_export"
	Layout LayoutDescription `json:"layout"`
}

type LayoutApplyResult struct {
	Type   string            `json:"type"` // "layout_apply"
	Layout LayoutDescription `json:"layout"`
}

type LayoutSplitRatioSetResult struct {
	Type   string            `json:"type"` // "layout_split_ratio_set"
	Layout LayoutDescription `json:"layout"`
}

// OutputMatch is the wait_for_output match spec.
type OutputMatch struct {
	Type  string `json:"type"` // "substring" | "regex"
	Value string `json:"value"`
}

// PaneWaitForOutputParams is pane.wait_for_output's request.
type PaneWaitForOutputParams struct {
	PaneID    string      `json:"pane_id"`
	Source    string      `json:"source"`
	Match     OutputMatch `json:"match"`
	Lines     *uint32     `json:"lines,omitempty"`
	StripAnsi *bool       `json:"strip_ansi,omitempty"`
	TimeoutMs *uint64     `json:"timeout_ms,omitempty"`
}

// OutputMatchedResult is pane.wait_for_output's success payload.
type OutputMatchedResult struct {
	Type        string          `json:"type"` // "output_matched"
	PaneID      string          `json:"pane_id"`
	Revision    uint64          `json:"revision"`
	MatchedLine string          `json:"matched_line"`
	Read        json.RawMessage `json:"read"`
}
