// Package model holds the application state and the pure command/apply
// layer. It owns no PTY, process, or I/O — those live in runtime and are
// driven through the effects returned by Apply.
package model

import (
	"fmt"
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
	Workspaces     []*Workspace
	Active         int // index into Workspaces
	SidebarVisible bool
	Cols, Rows     int // outer terminal size in cells
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

// Apply mutates the state for cmd and returns the effects the runtime must
// perform, in order.
func (s *AppState) Apply(cmd Command) []Effect {
	switch c := cmd.(type) {
	case CmdToggleWorktreeGroup:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].GroupCollapsed = !s.Workspaces[c.Index].GroupCollapsed
		}
		return nil
	case CmdSetWorktree:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].Worktree = c.Info
		}
		return nil
	case CmdMoveWorkspace:
		return s.moveWorkspace(c.Index, c.InsertIndex)
	case CmdNewWorkspace:
		ws := s.newWorkspace(c.Cwd)
		if !c.NoFocus {
			s.Active = len(s.Workspaces) - 1
		}
		return []Effect{
			EffSpawnPane{Pane: ws.Tabs[0].Layout.Focused(), Cwd: c.Cwd, Env: c.Env},
			EffSyncLayout{},
		}
	case CmdCloseWorkspace:
		return s.closeWorkspace(c.Index)
	case CmdSelectWorkspace:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Active = c.Index
			return []Effect{EffSyncLayout{}}
		}
	case CmdSplitFocused:
		ws := s.ActiveWorkspace()
		if ws == nil {
			return nil
		}
		l := ws.ActiveLayout()
		if l == nil {
			return nil
		}
		id := s.allocPane()
		l.SplitFocused(c.Dir, id)
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: ws.Cwd},
			EffSyncLayout{},
		}
	case CmdSplitPane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		ws := s.Workspaces[wi]
		l := ws.Tabs[ti].Layout
		id := s.allocPane()
		if !l.SplitAt(c.Pane, c.Dir, id, c.Ratio, c.Focus) {
			return nil
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: cwd, Env: c.Env},
			EffSyncLayout{},
		}
	case CmdRenameWorkspace:
		if c.Index >= 0 && c.Index < len(s.Workspaces) {
			s.Workspaces[c.Index].Name = c.Name
		}
	case CmdNewTab:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		tab := s.allocTab()
		ws.Tabs = append(ws.Tabs, tab)
		if !c.NoFocus {
			ws.ActiveTab = len(ws.Tabs) - 1
			s.Active = c.Workspace
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: tab.Layout.Focused(), Cwd: cwd, Env: c.Env},
			EffSyncLayout{},
		}
	case CmdSelectTab:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab >= 0 && c.Tab < len(ws.Tabs) {
			ws.ActiveTab = c.Tab
			s.Active = c.Workspace
			return []Effect{EffSyncLayout{}}
		}
	case CmdRenameTab:
		if c.Workspace >= 0 && c.Workspace < len(s.Workspaces) {
			ws := s.Workspaces[c.Workspace]
			if c.Tab >= 0 && c.Tab < len(ws.Tabs) {
				ws.Tabs[c.Tab].Name = c.Label
			}
		}
	case CmdCloseTab:
		return s.closeTab(c.Workspace, c.Tab)
	case CmdRenamePane:
		if s.PaneLabels == nil {
			s.PaneLabels = map[PaneID]string{}
		}
		if c.Label == "" {
			delete(s.PaneLabels, c.Pane)
		} else {
			s.PaneLabels[c.Pane] = c.Label
		}
	case CmdClosePane:
		return s.closePane(c.Pane)
	case CmdFocusPane:
		// Focus the pane's own workspace/tab, then the pane.
		if wi, ti := s.FindPane(c.Pane); wi >= 0 {
			ws := s.Workspaces[wi]
			s.Active = wi
			ws.ActiveTab = ti
			ws.Tabs[ti].Layout.Focus(c.Pane)
		}
	case CmdFocusDir:
		if ws := s.ActiveWorkspace(); ws != nil {
			if l := ws.ActiveLayout(); l != nil {
				l.FocusDir(c.Dir, s.PanesArea())
			}
		}
	case CmdCyclePane:
		if ws := s.ActiveWorkspace(); ws != nil {
			if l := ws.ActiveLayout(); l != nil {
				l.Cycle(c.Next)
			}
		}
	case CmdToggleSidebar:
		s.SidebarVisible = !s.SidebarVisible
		return []Effect{EffSyncLayout{}}
	case CmdResize:
		s.Cols, s.Rows = c.Cols, c.Rows
		return []Effect{EffSyncLayout{}}
	case CmdInput:
		return []Effect{EffInput{Pane: c.Pane, Bytes: c.Bytes}}
	case CmdPaneExited:
		return s.closePane(c.Pane)
	case CmdSplitImplicit:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab < 0 || c.Tab >= len(ws.Tabs) {
			return nil
		}
		l := ws.Tabs[c.Tab].Layout
		id := s.allocPane()
		ratio := c.Ratio
		if ratio <= 0 {
			ratio = 0.5
		}
		if !l.SplitImplicit(c.Dir, id, ratio, c.Focus) {
			return nil
		}
		cwd := c.Cwd
		if cwd == "" {
			cwd = ws.Cwd
		}
		return []Effect{
			EffSpawnPane{Pane: id, Cwd: cwd, Env: c.Env, Argv: c.Argv},
			EffSyncLayout{},
		}
	case CmdArrange:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		t := s.Workspaces[wi].Tabs[ti]
		switch c.Op {
		case "cycle":
			t.Layout.CyclePresets()
		case "equalize":
			t.Layout.Equalize()
		case "rotate_forward":
			t.Layout.Rotate(true)
		case "rotate_backward":
			t.Layout.Rotate(false)
		case "move_left":
			t.Layout.MoveToSide(c.Pane, layout.NavLeft)
		case "move_right":
			t.Layout.MoveToSide(c.Pane, layout.NavRight)
		case "move_up":
			t.Layout.MoveToSide(c.Pane, layout.NavUp)
		case "move_down":
			t.Layout.MoveToSide(c.Pane, layout.NavDown)
		default:
			return nil
		}
		return []Effect{EffSyncLayout{}}
	case CmdZoomPane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		t := s.Workspaces[wi].Tabs[ti]
		s.zoomPane(t, c.Pane, c.Mode, c.Out)
		return []Effect{EffSyncLayout{}}
	case CmdResizePane:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		changed := l.ResizeDir(c.Pane, c.Dir, c.Amount)
		if c.Out != nil {
			c.Out.Changed = changed
			if !changed {
				c.Out.Reason = "unchanged"
			}
		}
		return []Effect{EffSyncLayout{}}
	case CmdFocusPaneDir:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		nb := l.Neighbor(c.Pane, c.Dir, s.PanesArea())
		if c.Out != nil {
			if nb < 0 {
				c.Out.Reason = "no_neighbor"
				c.Out.Focus = l.Focused()
			} else {
				c.Out.Changed = true
				c.Out.Focus = nb
			}
		}
		if nb >= 0 {
			l.Focus(nb)
			return []Effect{EffSyncLayout{}}
		}
	case CmdSwapPanes:
		wiA, tiA := s.FindPane(c.A)
		wiB, tiB := s.FindPane(c.B)
		out := c.Out
		switch {
		case wiA < 0 || wiB < 0:
			if out != nil {
				out.Reason = "not_found"
				out.Target = -1
			}
		case wiA != wiB || tiA != tiB:
			if out != nil {
				out.Reason = "cross_tab"
				out.Target = -1
			}
		case c.A == c.B:
			if out != nil {
				out.Reason = "same_pane"
				out.Target = c.B
			}
		default:
			l := s.Workspaces[wiA].Tabs[tiA].Layout
			l.SwapLeaves(c.A, c.B)
			if out != nil {
				out.Changed = true
				out.Target = c.B
			}
			return []Effect{EffSyncLayout{}}
		}
	case CmdSwapDir:
		wi, ti := s.FindPane(c.Pane)
		if wi < 0 {
			if c.Out != nil {
				c.Out.Reason = "not_found"
				c.Out.Target = -1
			}
			return nil
		}
		l := s.Workspaces[wi].Tabs[ti].Layout
		nb := l.Neighbor(c.Pane, c.Dir, s.PanesArea())
		if c.Out != nil {
			c.Out.Target = nb
			if nb < 0 {
				c.Out.Reason = "no_neighbor"
			} else {
				c.Out.Changed = true
			}
		}
		if nb >= 0 {
			l.SwapLeaves(c.Pane, nb)
			return []Effect{EffSyncLayout{}}
		}
	case CmdMovePane:
		return s.movePane(c)
	case CmdSetSplitRatio:
		if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) {
			return nil
		}
		ws := s.Workspaces[c.Workspace]
		if c.Tab < 0 || c.Tab >= len(ws.Tabs) {
			return nil
		}
		ws.Tabs[c.Tab].Layout.SetRatioAt(c.Path, c.Ratio)
		return []Effect{EffSyncLayout{}}
	case CmdApplyLayout:
		return s.applyLayout(c)
	case CmdQuit:
		return []Effect{EffQuit{}}
	}
	return nil
}

// TabBarVisible reports whether the tab strip is drawn: on unless
// show_tab_bar=false, or a lone tab with hide_tab_bar_when_single_tab.
func (s *AppState) TabBarVisible() bool {
	if !s.ShowTabBar {
		return false
	}
	ws := s.ActiveWorkspace()
	if s.HideTabBarWhenSingleTab && ws != nil && len(ws.Tabs) <= 1 {
		return false
	}
	return true
}

// TabBarArea returns the one-row tab strip above the panes, or an empty
// rect when the bar is hidden.
func (s *AppState) TabBarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	if !s.TabBarVisible() {
		return Rect{}
	}
	return Rect{X: w, Y: 0, W: max0(s.Cols - w), H: 1}
}

// ActionBarArea is the one-row bottom pane action bar (SPEC G1): the
// main area's last row, right of the sidebar and above the status line.
// It shows only on desktop with an active workspace and at least two
// rows left for the panes.
func (s *AppState) ActionBarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	if s.ActiveWorkspace() == nil {
		return Rect{}
	}
	if s.Rows-3 < 2 { // need ≥2 pane rows: bar + status take the rest
		return Rect{}
	}
	return Rect{X: w, Y: max0(s.Rows - 2), W: max0(s.Cols - w), H: 1}
}

// PanesArea returns the rect the pane layout fills: right of the sidebar,
// below the (possibly hidden) tab bar, above the action bar and status.
func (s *AppState) PanesArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	y := 0
	if s.TabBarVisible() {
		y = 1
	}
	bottom := s.Rows - 1 // status line
	if ab := s.ActionBarArea(); ab.H > 0 {
		bottom = ab.Y
	}
	return Rect{X: w, Y: y, W: max0(s.Cols - w), H: max0(bottom - y)}
}

// SidebarArea returns the sidebar rect (may be the collapsed strip).
func (s *AppState) SidebarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	return Rect{X: 0, Y: 0, W: min(s.Cols, w), H: max0(s.Rows - 1)}
}

// StatusArea returns the one-line status bar rect.
func (s *AppState) StatusArea() Rect {
	return Rect{X: 0, Y: max0(s.Rows - 1), W: s.Cols, H: 1}
}

const (
	// SidebarWidth is the expanded sidebar width in cells.
	SidebarWidth = 28
	// SidebarCollapsedWidth is the collapsed strip width in cells.
	SidebarCollapsedWidth = 3
)

// Rect re-exports the layout rect for convenience.
type Rect = layout.Rect

// PaneContentRect resolves a pane rect to its terminal-content rect and
// gutter column count per SPEC G1: a frame eats one cell on each side but
// only when borders are on and the pane is wider and taller than 2; the
// rightmost inner column is a scrollbar gutter whenever the framed inner
// width is at least 5.
func PaneContentRect(r Rect, borders bool) (inner Rect, gutter int) {
	if !borders || r.W <= 2 || r.H <= 2 {
		return r, 0
	}
	inner = Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
	if inner.W >= 5 {
		gutter = 1
	}
	return inner, gutter
}

// PaneTermSize is the PTY cell size inside a pane rect: the content rect
// minus its gutter column.
func PaneTermSize(r Rect, borders bool) (w, h int) {
	inner, gutter := PaneContentRect(r, borders)
	return max(1, inner.W-gutter), max(1, inner.H)
}

// --- internals ---

func (s *AppState) allocPane() PaneID {
	s.nextPaneID++
	return s.nextPaneID
}

func (s *AppState) allocTab() *Tab {
	s.nextTabID++
	return &Tab{ID: s.nextTabID, Layout: layout.New(s.allocPane())}
}

func (s *AppState) newWorkspace(cwd string) *Workspace {
	s.nextWorkspaceID++
	ws := &Workspace{
		ID:   s.nextWorkspaceID,
		Name: fmt.Sprintf("workspace %d", s.nextWorkspaceID),
		Cwd:  cwd,
	}
	ws.Tabs = []*Tab{s.allocTab()}
	s.Workspaces = append(s.Workspaces, ws)
	return ws
}

// TabLabel is the display name: the stored name or "tab N".
func (t *Tab) TabLabel() string {
	if t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("tab %d", t.ID)
}

func (s *AppState) closeWorkspace(idx int) []Effect {
	if idx < 0 || idx >= len(s.Workspaces) {
		return nil
	}
	ws := s.Workspaces[idx]
	var effs []Effect
	for _, t := range ws.Tabs {
		for _, id := range t.Layout.PaneIDs() {
			effs = append(effs, EffKillPane{Pane: id})
		}
	}
	s.Workspaces = append(s.Workspaces[:idx], s.Workspaces[idx+1:]...)
	if len(s.Workspaces) == 0 {
		return append(effs, EffQuit{})
	}
	if s.Active > idx {
		s.Active--
	}
	if s.Active >= len(s.Workspaces) {
		s.Active = len(s.Workspaces) - 1
	}
	return append(effs, EffSyncLayout{})
}

func (s *AppState) closeTab(wi, ti int) []Effect {
	if wi < 0 || wi >= len(s.Workspaces) {
		return nil
	}
	ws := s.Workspaces[wi]
	if ti < 0 || ti >= len(ws.Tabs) {
		return nil
	}
	var effs []Effect
	for _, id := range ws.Tabs[ti].Layout.PaneIDs() {
		effs = append(effs, EffKillPane{Pane: id})
	}
	ws.Tabs = append(ws.Tabs[:ti], ws.Tabs[ti+1:]...)
	if len(ws.Tabs) == 0 {
		// The last tab of a workspace takes the workspace with it.
		s.Workspaces = append(s.Workspaces[:wi], s.Workspaces[wi+1:]...)
		if len(s.Workspaces) == 0 {
			return append(effs, EffQuit{})
		}
		if s.Active > wi {
			s.Active--
		}
		if s.Active >= len(s.Workspaces) {
			s.Active = len(s.Workspaces) - 1
		}
		return append(effs, EffSyncLayout{})
	}
	if ws.ActiveTab > ti {
		ws.ActiveTab--
	}
	if ws.ActiveTab >= len(ws.Tabs) {
		ws.ActiveTab = len(ws.Tabs) - 1
	}
	return append(effs, EffSyncLayout{})
}

func (s *AppState) closePane(pane PaneID) []Effect {
	wi, ti := s.FindPane(pane)
	if wi < 0 {
		return nil
	}
	ws := s.Workspaces[wi]
	var effs []Effect
	effs = append(effs, EffKillPane{Pane: pane})
	if ws.Tabs[ti].Zoomed == pane {
		ws.Tabs[ti].Zoomed = 0
	}
	if !ws.Tabs[ti].Layout.Close(pane) {
		// Last pane of the tab closes the tab; the last tab takes the
		// workspace (and possibly the app) with it.
		return append(effs, s.closeTab(wi, ti)...)
	}
	return append(effs, EffSyncLayout{})
}

// zoomPane implements CmdZoomPane on the pane's own tab.
func (s *AppState) zoomPane(t *Tab, pane PaneID, mode string, out *ZoomOutcome) {
	if out == nil {
		out = &ZoomOutcome{}
	}
	single := t.Layout.PaneCount() <= 1
	if single {
		// A one-pane tab can never zoom: the op only (re)focuses the pane
		// and reports why nothing else happened.
		fc := t.Layout.Focused() != pane
		if fc {
			t.Layout.Focus(pane)
		}
		*out = ZoomOutcome{Changed: fc, FocusChanged: fc, Reason: "single_pane"}
		return
	}
	zoomed := t.Zoomed == pane
	anyZoom := t.Zoomed != 0
	switch mode {
	case "on":
		if zoomed {
			*out = ZoomOutcome{Changed: false, Zoomed: true, Reason: "already_zoomed"}
			return
		}
		t.Zoomed = pane
		fc := t.Layout.Focused() != pane
		t.Layout.Focus(pane)
		*out = ZoomOutcome{Changed: true, ZoomChanged: true, FocusChanged: fc, Zoomed: true}
	case "off":
		if !anyZoom {
			*out = ZoomOutcome{Changed: false, Reason: "already_unzoomed"}
			return
		}
		t.Zoomed = 0
		*out = ZoomOutcome{Changed: true, ZoomChanged: true}
	case "", "toggle":
		if zoomed {
			t.Zoomed = 0
			*out = ZoomOutcome{Changed: true, ZoomChanged: true}
			return
		}
		t.Zoomed = pane
		fc := t.Layout.Focused() != pane
		t.Layout.Focus(pane)
		*out = ZoomOutcome{Changed: true, ZoomChanged: true, FocusChanged: fc, Zoomed: true}
	}
}

// movePane implements CmdMovePane: the pane keeps its id and process while
// its leaf slot migrates to another tab/workspace.
func (s *AppState) movePane(c CmdMovePane) []Effect {
	wi, ti := s.FindPane(c.Pane)
	if wi < 0 {
		return nil
	}
	src := s.Workspaces[wi].Tabs[ti]
	out := c.Out
	if out != nil {
		out.Pane = c.Pane
		out.PrevWsID, out.PrevTabID = s.Workspaces[wi].ID, src.ID
		out.TgtWsIdx, out.TgtTabIdx, out.NewWsIdx = -1, -1, -1
	}
	if src.Zoomed != 0 {
		if out != nil {
			out.Reason = "zoomed_tab"
		}
		return nil
	}
	switch c.Dest.Kind {
	case MoveDestTab:
		if c.Dest.TabW < 0 || c.Dest.TabW >= len(s.Workspaces) {
			return nil
		}
		dws := s.Workspaces[c.Dest.TabW]
		if c.Dest.TabT < 0 || c.Dest.TabT >= len(dws.Tabs) {
			return nil
		}
		dst := dws.Tabs[c.Dest.TabT]
		if dst == src {
			if out != nil {
				out.Reason = "same_tab"
			}
			return nil
		}
		if dst.Zoomed != 0 {
			if out != nil {
				out.Reason = "zoomed_tab"
			}
			return nil
		}
		dwi, dti := c.Dest.TabW, c.Dest.TabT
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		if out != nil && out.ClosedWsID != 0 && dwi > wi {
			dwi--
		} else if out != nil && out.ClosedTabID != 0 && dwi == wi && dti > ti {
			// Source tab dissolved inside the destination workspace.
			dti--
		}
		target := c.Dest.Target
		if !dst.Layout.HasPane(target) {
			target = dst.Layout.LastLeaf()
		}
		ratio := c.Dest.Ratio
		if ratio <= 0 {
			ratio = 0.5
		}
		dst.Layout.SplitAt(target, c.Dest.Split, c.Pane, ratio, false)
		if c.Focus {
			dst.Layout.Focus(c.Pane)
		}
		if out != nil {
			out.Changed = true
			out.TgtWsID, out.TgtTabID = dws.ID, dst.ID
			out.TgtWsIdx, out.TgtTabIdx = dwi, dti
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	case MoveDestNewTab:
		dw := c.Dest.Workspace
		if dw < 0 {
			dw = wi
		}
		if dw < 0 || dw >= len(s.Workspaces) {
			return nil
		}
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		if out != nil && out.ClosedWsID != 0 && dw > wi {
			dw--
		}
		ws := s.Workspaces[dw]
		tab := s.allocTabFrom(c.Pane)
		tab.Name = c.Dest.Label
		ws.Tabs = append(ws.Tabs, tab)
		if out != nil {
			out.Changed = true
			out.NewTabID = tab.ID
			out.TgtWsID, out.TgtTabID = ws.ID, tab.ID
			out.TgtWsIdx, out.TgtTabIdx = dw, len(ws.Tabs)-1
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	case MoveDestNewWorkspace:
		s.dropPaneFromTab(wi, ti, c.Pane, out)
		s.nextWorkspaceID++
		ws := &Workspace{
			ID:   s.nextWorkspaceID,
			Name: c.Dest.Label,
			Cwd:  c.Dest.Cwd,
		}
		if ws.Name == "" {
			ws.Name = fmt.Sprintf("workspace %d", ws.ID)
		}
		tab := s.allocTabFrom(c.Pane)
		tab.Name = c.Dest.TabLabel
		ws.Tabs = []*Tab{tab}
		s.Workspaces = append(s.Workspaces, ws)
		if out != nil {
			out.Changed = true
			out.NewWsID, out.NewTabID = ws.ID, tab.ID
			out.NewWsIdx = len(s.Workspaces) - 1
			out.TgtWsID, out.TgtTabID = ws.ID, tab.ID
			out.TgtWsIdx, out.TgtTabIdx = out.NewWsIdx, 0
		}
		s.finishMoveFocus(c, out)
		return []Effect{EffSyncLayout{}}
	}
	return nil
}

// dropPaneFromTab removes pane's leaf from its tab without killing the
// process; an emptied tab/workspace is removed from the model and recorded
// in out.
func (s *AppState) dropPaneFromTab(wi, ti int, pane PaneID, out *PaneMoveOutcome) {
	ws := s.Workspaces[wi]
	ws.Tabs[ti].Layout.RemoveLeaf(pane)
	if len(ws.Tabs[ti].Layout.PaneIDs()) > 0 {
		return
	}
	// Empty tab dissolves; an emptied workspace dissolves with it.
	ws.Tabs = append(ws.Tabs[:ti], ws.Tabs[ti+1:]...)
	if out != nil {
		out.ClosedTabID = out.PrevTabID
	}
	if len(ws.Tabs) == 0 {
		s.Workspaces = append(s.Workspaces[:wi], s.Workspaces[wi+1:]...)
		if out != nil {
			out.ClosedWsID = out.PrevWsID
		}
		return
	}
	if ws.ActiveTab >= len(ws.Tabs) {
		ws.ActiveTab = len(ws.Tabs) - 1
	}
}

// finishMoveFocus applies the focus flag: focus=true activates the
// destination workspace and tab; otherwise the current selection survives
// (indices may have shifted if a workspace closed).
func (s *AppState) finishMoveFocus(c CmdMovePane, out *PaneMoveOutcome) {
	if c.Focus && out != nil && out.TgtWsIdx >= 0 {
		s.Active = out.TgtWsIdx
		s.Workspaces[out.TgtWsIdx].ActiveTab = out.TgtTabIdx
	}
	if len(s.Workspaces) == 0 {
		return
	}
	if s.Active >= len(s.Workspaces) {
		s.Active = len(s.Workspaces) - 1
	}
	if !c.Focus {
		ws := s.Workspaces[s.Active]
		if ws.ActiveTab >= len(ws.Tabs) {
			ws.ActiveTab = len(ws.Tabs) - 1
		}
	}
}

// allocTabFrom wraps an existing pane id in a fresh single-pane tab.
func (s *AppState) allocTabFrom(pane PaneID) *Tab {
	s.nextTabID++
	return &Tab{ID: s.nextTabID, Layout: layout.Restore(layout.LeafNode(pane), pane, []int{pane})}
}

// applyLayout implements CmdApplyLayout: optionally close the target tab,
// then append a new tab whose tree is built from the spec, spawning a
// process per leaf.
func (s *AppState) applyLayout(c CmdApplyLayout) []Effect {
	if c.Workspace < 0 || c.Workspace >= len(s.Workspaces) || c.Root == nil {
		return nil
	}
	wsCwd := s.Workspaces[c.Workspace].Cwd
	var effs []Effect
	wi := c.Workspace
	if c.Tab >= 0 {
		effs = append(effs, s.closeTab(wi, c.Tab)...)
		if wi >= len(s.Workspaces) {
			// The replaced tab was its workspace's last: recreate an
			// otherwise-empty workspace so the applied tab has a home.
			// closeTab emitted EffQuit for the empty workspace set; the
			// apply keeps the app alive, so drop it.
			kept := effs[:0]
			for _, e := range effs {
				if _, quit := e.(EffQuit); !quit {
					kept = append(kept, e)
				}
			}
			effs = kept
			s.nextWorkspaceID++
			s.Workspaces = append(s.Workspaces, &Workspace{
				ID:   s.nextWorkspaceID,
				Name: fmt.Sprintf("workspace %d", s.nextWorkspaceID),
				Cwd:  wsCwd,
			})
			wi = len(s.Workspaces) - 1
		}
	}
	ws := s.Workspaces[wi]
	s.nextTabID++
	root, ok := s.buildApplyTree(c.Root, &effs, ws.Cwd)
	if !ok {
		return nil
	}
	var ids []int
	collectApplyLeaves(root, &ids)
	if len(ids) == 0 {
		return nil
	}
	tab := &Tab{ID: s.nextTabID, Name: c.TabLabel, Layout: layout.Restore(root, ids[0], ids)}
	ws.Tabs = append(ws.Tabs, tab)
	tabIdx := len(ws.Tabs) - 1
	if c.Out != nil {
		c.Out.WsIdx = wi
		c.Out.TabIdx = tabIdx
		c.Out.PaneIDs = ids
	}
	if c.Focus {
		s.Active = wi
		ws.ActiveTab = tabIdx
	}
	return append(effs, EffSyncLayout{})
}

// buildApplyTree allocates a pane id per leaf and queues its spawn effect.
func (s *AppState) buildApplyTree(n *ApplyNode, effs *[]Effect, wsCwd string) (*layout.Node, bool) {
	if n == nil {
		return nil, false
	}
	if n.Leaf != nil {
		id := s.allocPane()
		cwd := n.Leaf.Cwd
		if cwd == "" {
			cwd = wsCwd
		}
		*effs = append(*effs, EffSpawnPane{Pane: id, Cwd: cwd, Env: n.Leaf.Env, Argv: n.Leaf.Argv})
		if n.Leaf.Label != "" {
			if s.PaneLabels == nil {
				s.PaneLabels = map[PaneID]string{}
			}
			s.PaneLabels[id] = n.Leaf.Label
		}
		return layout.LeafNode(id), true
	}
	first, ok1 := s.buildApplyTree(n.First, effs, wsCwd)
	second, ok2 := s.buildApplyTree(n.Second, effs, wsCwd)
	if !ok1 || !ok2 {
		return nil, false
	}
	return layout.SplitNode(n.Dir, n.Ratio, first, second), true
}

func collectApplyLeaves(n *layout.Node, out *[]int) {
	if n == nil {
		return
	}
	if n.Leaf >= 0 {
		*out = append(*out, n.Leaf)
		return
	}
	collectApplyLeaves(n.First, out)
	collectApplyLeaves(n.Second, out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// --- command-diff snapshot (domain events) ---

// PaneRef records where a pane lived when a StateSnap was taken.
type PaneRef struct {
	WSIdx  int
	TabIdx int
	WSID   WorkspaceID
	TabID  int
}

// TabSnap captures the diffable parts of a tab.
type TabSnap struct {
	ID      int
	Name    string
	Focused PaneID
	FP      string // layout tree fingerprint (structure+ratios+focus)
	Panes   int
}

// WsSnap captures the diffable parts of a workspace.
type WsSnap struct {
	Worktree       *api.WorkspaceWorktreeInfo
	GroupCollapsed bool
	ID             WorkspaceID
	Name           string
	Cwd            string
	ActiveTab      int
	Tabs           []TabSnap
	Panes          int
}

// StateSnap is a lightweight pre/post command snapshot used by the server
// to derive domain events (created/closed/renamed/focused/moved/updated).
type StateSnap struct {
	Active      int
	Workspaces  []WsSnap
	Panes       map[PaneID]PaneRef
	FocusedPane PaneID
}

// Capture records the current state for post-command diffing.
func (s *AppState) Capture() *StateSnap {
	snap := &StateSnap{
		Active:      s.Active,
		Panes:       map[PaneID]PaneRef{},
		FocusedPane: s.FocusedPane(),
	}
	for i, ws := range s.Workspaces {
		wss := WsSnap{Worktree: ws.Worktree, GroupCollapsed: ws.GroupCollapsed, ID: ws.ID, Name: ws.Name, Cwd: ws.Cwd, ActiveTab: ws.ActiveTab}
		for j, t := range ws.Tabs {
			wss.Tabs = append(wss.Tabs, TabSnap{
				ID: t.ID, Name: t.Name, Focused: t.Layout.Focused(),
				FP: t.Layout.Fingerprint(), Panes: t.Layout.PaneCount(),
			})
			for _, pid := range t.Layout.PaneIDs() {
				snap.Panes[pid] = PaneRef{WSIdx: i, TabIdx: j, WSID: ws.ID, TabID: t.ID}
			}
			wss.Panes += t.Layout.PaneCount()
		}
		snap.Workspaces = append(snap.Workspaces, wss)
	}
	return snap
}

// PaneRects returns display pane rects for the tab: a zoomed pane occupies
// the whole area (SPEC G1 zoom), otherwise the layout's normal split rects.
// API layout snapshots still use Layout.Panes (zoomed reports the tree).
func (t *Tab) PaneRects(area Rect) []layout.PaneRect {
	if t.Zoomed != 0 && t.Layout.HasPane(t.Zoomed) {
		return []layout.PaneRect{{ID: t.Zoomed, Rect: area, IsFocused: true}}
	}
	return t.Layout.Panes(area)
}
