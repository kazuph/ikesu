package model

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
)

// PaneID identifies a pane across the whole app (not per workspace).
type PaneID = int

// WorkspaceID identifies a workspace across the whole app.
type WorkspaceID = int

// Tab is one split layout inside a workspace.
type Tab struct {
	ID     int
	Name   string // empty → auto label
	Layout *layout.Layout
	// Zoomed is the pane id temporarily occupying the whole pane area, or 0.
	Zoomed PaneID
}

// Workspace is a named set of tabs, each holding its own pane layout.
type Workspace struct {
	Worktree       *api.WorkspaceWorktreeInfo
	GroupCollapsed bool
	Section        Section
	CustomName     bool
	ID             WorkspaceID
	Name           string
	Cwd            string
	Tabs           []*Tab
	ActiveTab      int
}

// TabRef returns the workspace's active tab, or nil.
func (ws *Workspace) TabRef() *Tab {
	if ws.ActiveTab < 0 || ws.ActiveTab >= len(ws.Tabs) {
		return nil
	}
	return ws.Tabs[ws.ActiveTab]
}

// ActiveLayout returns the active tab's layout, or nil.
func (ws *Workspace) ActiveLayout() *layout.Layout {
	if t := ws.TabRef(); t != nil {
		return t.Layout
	}
	return nil
}

// PaneCount counts panes across all tabs.
func (ws *Workspace) PaneCount() int {
	n := 0
	for _, t := range ws.Tabs {
		n += t.Layout.PaneCount()
	}
	return n
}

// AppState is the complete pure application state.
type AppState struct {
	Workspaces                                          []*Workspace
	Active                                              int // index into Workspaces
	SidebarVisible                                      bool
	SidebarSize, SidebarMin, SidebarMax, SidebarDefault int
	SidebarManual                                       bool
	CollapsedSections                                   map[Section]bool
	WorkspaceDensity, AgentScope                        string
	Cols, Rows                                          int // outer terminal size in cells
	// UI flags from [ui] config (SPEC G1): the tab bar defaults on,
	// pane frames default on, single-tab hiding defaults off.
	ShowTabBar              bool
	HideTabBarWhenSingleTab bool
	PaneBorders             bool
	// PaneLabels holds optional API-assigned pane names (pane.rename).
	PaneLabels map[PaneID]string

	nextPaneID      PaneID
	nextWorkspaceID WorkspaceID
	nextTabID       int
}

// New returns an AppState with one workspace containing one pane.
func New(cwd string, cols, rows int) *AppState {
	s := &AppState{
		SidebarVisible: true, Cols: cols, Rows: rows,
		ShowTabBar: true, PaneBorders: true,
	}
	s.newWorkspace(cwd)
	return s
}

// ActiveWorkspace returns the current workspace or nil.
func (s *AppState) ActiveWorkspace() *Workspace {
	if s.Active < 0 || s.Active >= len(s.Workspaces) {
		return nil
	}
	return s.Workspaces[s.Active]
}

// FocusedPane returns the focused pane of the active workspace.
func (s *AppState) FocusedPane() PaneID {
	if ws := s.ActiveWorkspace(); ws != nil {
		if t := ws.TabRef(); t != nil {
			return t.Layout.Focused()
		}
	}
	return -1
}

// WorkspaceOf returns the index of the workspace owning pane.
func (s *AppState) WorkspaceOf(pane PaneID) int {
	wi, _ := s.FindPane(pane)
	return wi
}

// PeekNextTabID returns the id the next created tab will get.
func (s *AppState) PeekNextTabID() int { return s.nextTabID + 1 }

// FindPane locates a pane, returning workspace and tab indices.
func (s *AppState) FindPane(pane PaneID) (wsIdx, tabIdx int) {
	for i, ws := range s.Workspaces {
		for j, t := range ws.Tabs {
			for _, id := range t.Layout.PaneIDs() {
				if id == pane {
					return i, j
				}
			}
		}
	}
	return -1, -1
}

// Command is a user or runtime intent against the pure state.
type Command interface{ isCommand() }

type (
	// CmdNewWorkspace creates a workspace whose single pane starts in Cwd;
	// the new workspace takes focus unless NoFocus is set.
	CmdNewWorkspace struct {
		Cwd     string
		Env     map[string]string
		NoFocus bool
		Section Section
	}
	// CmdCloseWorkspace closes workspace idx.
	CmdCloseWorkspace struct{ Index int }
	// CmdSelectWorkspace switches to workspace idx.
	CmdSelectWorkspace struct{ Index int }
	// CmdSplitFocused splits the focused pane in direction dir.
	CmdSplitFocused struct{ Dir layout.Direction }
	// CmdSplitPane splits Pane of workspace index at Ratio; Focus controls
	// whether the new pane takes focus.
	CmdSplitPane struct {
		Workspace int
		Pane      PaneID
		Dir       layout.Direction
		Ratio     float64
		Focus     bool
		Cwd       string
		Env       map[string]string
	}
	// CmdRenameWorkspace renames workspace idx.
	CmdRenameWorkspace struct {
		Index int
		Name  string
	}
	// CmdNewTab adds a tab to workspace index with one pane starting in
	// Cwd; the tab takes focus unless NoFocus is set.
	CmdNewTab struct {
		Workspace int
		Cwd       string
		Env       map[string]string
		NoFocus   bool
	}
	// CmdSelectTab focuses tab index of workspace index (also focuses the
	// workspace).
	CmdSelectTab struct {
		Workspace int
		Tab       int
	}
	// CmdRenameTab renames a tab ("" restores the auto label).
	CmdRenameTab struct {
		Workspace int
		Tab       int
		Label     string
	}
	// CmdCloseTab closes a tab and kills its panes; closing the last tab
	// closes the workspace.
	CmdCloseTab struct {
		Workspace int
		Tab       int
	}
	// CmdRenamePane sets a pane's label ("" clears).
	CmdRenamePane struct {
		Pane  PaneID
		Label string
	}
	// CmdClosePane closes a pane.
	CmdClosePane struct{ Pane PaneID }
	// CmdFocusPane focuses a pane of the active workspace.
	CmdFocusPane struct{ Pane PaneID }
	// CmdFocusDir moves focus to the neighbour in dir.
	CmdFocusDir struct{ Dir layout.NavDir }
	// CmdCyclePane cycles focus through panes of the active workspace.
	CmdCyclePane struct{ Next bool }
	// CmdToggleSidebar toggles the sidebar.
	CmdToggleSidebar struct{}
	// CmdResize records the outer terminal size.
	CmdResize struct{ Cols, Rows int }
	// CmdInput feeds bytes to a pane.
	CmdInput struct {
		Pane  PaneID
		Bytes []byte
	}
	// CmdQuit requests application exit.
	CmdQuit struct{}
	// CmdPaneExited is generated by the runtime when a pane process dies.
	CmdPaneExited struct{ Pane PaneID }
	// CmdSplitImplicit splits the last leaf of a tab in traversal order —
	// the non-explicit pane.split / agent.start contract (SPEC G1).
	CmdSplitImplicit struct {
		Workspace int
		Tab       int
		Dir       layout.Direction
		Ratio     float64
		Focus     bool
		Cwd       string
		Env       map[string]string
		Argv      []string
	}
	// CmdArrange performs a same-tab arrangement op on Pane's own tab.
	// Op is one of: cycle, equalize, rotate_forward, rotate_backward,
	// move_left, move_right, move_up, move_down.
	CmdArrange struct {
		Pane PaneID
		Op   string
	}
	// CmdZoomPane applies pane zoom mode ("toggle"|"on"|"off") in Pane's
	// own tab. Out carries the outcome when non-nil.
	CmdZoomPane struct {
		Pane PaneID
		Mode string
		Out  *ZoomOutcome
	}
	// CmdResizePane adjusts the nearest same-axis split of Pane by Amount
	// (ratio delta, sign resolved from Dir); Amount<=0 means the 0.05 step.
	CmdResizePane struct {
		Pane   PaneID
		Dir    layout.NavDir
		Amount float64
		Out    *ResizeOutcome
	}
	// CmdFocusPaneDir moves focus from Pane to its neighbour in Dir inside
	// Pane's own tab (pane.focus_direction); it never switches workspace
	// or tab. Out is non-nil for API callers.
	CmdFocusPaneDir struct {
		Pane PaneID
		Dir  layout.NavDir
		Out  *FocusDirOutcome
	}
	// CmdSwapPanes exchanges two leaf slots (same tab only).
	CmdSwapPanes struct {
		A, B PaneID
		Out  *SwapOutcome
	}
	// CmdSwapDir swaps Pane with its neighbour in Dir (same tab only).
	CmdSwapDir struct {
		Pane PaneID
		Dir  layout.NavDir
		Out  *SwapOutcome
	}
	// CmdMovePane relocates Pane to another tab/workspace (SPEC G1 pane
	// move). The pane keeps its id and process. Out carries the result.
	CmdMovePane struct {
		Pane  PaneID
		Dest  MoveDest
		Focus bool
		Out   *PaneMoveOutcome
	}
	// CmdSetSplitRatio sets the ratio of the split at Path inside a tab.
	CmdSetSplitRatio struct {
		Workspace int
		Tab       int
		Path      []bool
		Ratio     float64
	}
	// CmdApplyLayout replaces the tab at Tab (>=0 closes it first) or
	// creates a fresh tab when Tab<0, materializing Root's leaf panes as
	// new processes. Out carries the created tab/pane ids.
	CmdApplyLayout struct {
		Workspace int
		Tab       int // -1 → append a new tab
		TabLabel  string
		Focus     bool
		Root      *ApplyNode
		Out       *ApplyOutcome
	}
)

// MoveDestKind selects the pane.move destination variant.
type MoveDestKind int

const (
	MoveDestTab MoveDestKind = iota
	MoveDestNewTab
	MoveDestNewWorkspace
)

// MoveDest describes where pane.move places a pane.
type MoveDest struct {
	Kind      MoveDestKind
	TabW      int // MoveDestTab: destination workspace index
	TabT      int // MoveDestTab: destination tab index
	Workspace int // MoveDestNewTab: destination workspace index
	Split     layout.Direction
	Target    PaneID // MoveDestTab: explicit split target; 0 → traversal-last leaf
	Ratio     float64
	Label     string // MoveDestNewWorkspace: workspace label / MoveDestNewTab: tab label
	TabLabel  string // MoveDestNewWorkspace: tab label
	Cwd       string // MoveDestNewWorkspace: workspace cwd
}

// PaneMoveOutcome reports a pane.move. Ids are public (Workspace.ID,
// Tab.ID) so the server can format them after indices shifted; indices
// locate the destination tab/workspace post-move. Zero id means none.
type PaneMoveOutcome struct {
	Changed     bool
	Reason      string // "" | "same_tab" | "zoomed_tab"
	Pane        PaneID
	PrevWsID    int
	PrevTabID   int
	TgtWsID     int
	TgtTabID    int
	TgtWsIdx    int // post-move destination indices (-1 when unchanged)
	TgtTabIdx   int
	ClosedWsID  int // public id of the dissolved workspace, 0 none
	ClosedTabID int // public id of the dissolved tab, 0 none
	NewWsID     int
	NewTabID    int
	NewWsIdx    int
}

// ZoomOutcome is filled by CmdZoomPane.
type ZoomOutcome struct {
	Changed      bool
	ZoomChanged  bool
	FocusChanged bool
	Zoomed       bool
	Reason       string // "" | "single_pane" | "already_zoomed" | "already_unzoomed"
}

// ResizeOutcome is filled by CmdResizePane.
type ResizeOutcome struct {
	Changed bool
	Reason  string // "" | "unchanged"
}

// FocusDirOutcome is filled by CmdFocusPaneDir.
type FocusDirOutcome struct {
	Changed bool
	Focus   PaneID // -1 when unchanged
	Reason  string // "" | "no_neighbor"
}

// SwapOutcome is filled by CmdSwapPanes/CmdSwapDir.
type SwapOutcome struct {
	Changed bool
	Target  PaneID // -1 none
	Reason  string // "" | "no_neighbor" | "same_pane" | "not_found" | "cross_tab"
}

// ApplyNode is the model-side layout.apply tree (leaves spawn processes).
type ApplyNode struct {
	Leaf   *ApplyLeaf
	Dir    layout.Direction
	Ratio  float64
	First  *ApplyNode
	Second *ApplyNode
}

// ApplyLeaf carries per-leaf spawn attributes.
type ApplyLeaf struct {
	Cwd   string
	Env   map[string]string
	Label string
	Argv  []string
}

// ApplyOutcome is filled by CmdApplyLayout.
type ApplyOutcome struct {
	WsIdx   int
	TabIdx  int
	PaneIDs []int
}

func (CmdNewWorkspace) isCommand()    {}
func (CmdRenameWorkspace) isCommand() {}
func (CmdNewTab) isCommand()          {}
func (CmdSelectTab) isCommand()       {}
func (CmdRenameTab) isCommand()       {}
func (CmdCloseTab) isCommand()        {}
func (CmdRenamePane) isCommand()      {}
func (CmdSplitPane) isCommand()       {}
func (CmdCloseWorkspace) isCommand()  {}
func (CmdSelectWorkspace) isCommand() {}
func (CmdSplitFocused) isCommand()    {}
func (CmdClosePane) isCommand()       {}
func (CmdFocusPane) isCommand()       {}
func (CmdFocusDir) isCommand()        {}
func (CmdCyclePane) isCommand()       {}
func (CmdToggleSidebar) isCommand()   {}
func (CmdResize) isCommand()          {}
func (CmdInput) isCommand()           {}
func (CmdQuit) isCommand()            {}
func (CmdPaneExited) isCommand()      {}
func (CmdSplitImplicit) isCommand()   {}
func (CmdArrange) isCommand()         {}
func (CmdZoomPane) isCommand()        {}
func (CmdResizePane) isCommand()      {}
func (CmdFocusPaneDir) isCommand()    {}
func (CmdSwapPanes) isCommand()       {}
func (CmdSwapDir) isCommand()         {}
func (CmdMovePane) isCommand()        {}
func (CmdSetSplitRatio) isCommand()   {}
func (CmdApplyLayout) isCommand()     {}

// Effect is a side effect the runtime must perform after Apply.
type Effect interface{ isEffect() }

type (
	// EffSpawnPane starts a shell in Cwd for pane id; Env adds per-pane
	// environment (API-created panes). Argv, when non-empty, runs that
	// command instead of the login shell (layout.apply leaf command).
	EffSpawnPane struct {
		Pane PaneID
		Cwd  string
		Env  map[string]string
		Argv []string
	}
	// EffKillPane terminates the pane process.
	EffKillPane struct{ Pane PaneID }
	// EffSyncLayout asks the runtime to resize every pane to its rect.
	EffSyncLayout struct{}
	// EffInput forwards bytes to the pane's PTY.
	EffInput struct {
		Pane  PaneID
		Bytes []byte
	}
	// EffQuit exits the application.
	EffQuit struct{}
)

func (EffSpawnPane) isEffect()  {}
func (EffKillPane) isEffect()   {}
func (EffSyncLayout) isEffect() {}
func (EffInput) isEffect()      {}
func (EffQuit) isEffect()       {}
