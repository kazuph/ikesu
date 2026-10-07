// Package model holds the application state and the pure command/apply
// layer. It owns no PTY, process, or I/O — those live in runtime and are
// driven through the effects returned by Apply.
package model

import (
	"fmt"

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
}

// Workspace is a named set of tabs, each holding its own pane layout.
type Workspace struct {
	ID        WorkspaceID
	Name      string
	Cwd       string
	Tabs      []*Tab
	ActiveTab int
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
	// PaneLabels holds optional API-assigned pane names (pane.rename).
	PaneLabels map[PaneID]string

	nextPaneID      PaneID
	nextWorkspaceID WorkspaceID
	nextTabID       int
}

// New returns an AppState with one workspace containing one pane.
func New(cwd string, cols, rows int) *AppState {
	s := &AppState{SidebarVisible: true, Cols: cols, Rows: rows}
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
)

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

// Effect is a side effect the runtime must perform after Apply.
type Effect interface{ isEffect() }

type (
	// EffSpawnPane starts a shell in Cwd for pane id; Env adds per-pane
	// environment (API-created panes).
	EffSpawnPane struct {
		Pane PaneID
		Cwd  string
		Env  map[string]string
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
	case CmdQuit:
		return []Effect{EffQuit{}}
	}
	return nil
}

// TabBarArea returns the one-row tab strip above the panes.
func (s *AppState) TabBarArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	return Rect{X: w, Y: 0, W: max0(s.Cols - w), H: 1}
}

// PanesArea returns the rect the pane layout fills: right of the sidebar,
// below the tab bar, above the status line.
func (s *AppState) PanesArea() Rect {
	w := SidebarCollapsedWidth
	if s.SidebarVisible {
		w = SidebarWidth
	}
	h := s.Rows - 2 // tab bar + status line
	return Rect{X: w, Y: 1, W: max0(s.Cols - w), H: max0(h)}
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
	if !ws.Tabs[ti].Layout.Close(pane) {
		// Last pane of the tab closes the tab; the last tab takes the
		// workspace (and possibly the app) with it.
		return append(effs, s.closeTab(wi, ti)...)
	}
	return append(effs, EffSyncLayout{})
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
