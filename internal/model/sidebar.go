package model

import "github.com/kazuph/goterrace/internal/layout"

type Section string

const (
	SectionNone          Section = "none"
	SectionFavorite      Section = "favorite"
	SectionWork          Section = "work"
	SectionPersonal      Section = "personal"
	SidebarMinDefault            = 18
	SidebarMaxDefault            = 72
	SidebarNormalDefault         = 26
)

var SectionOrder = []Section{SectionFavorite, SectionWork, SectionPersonal, SectionNone}

func (s Section) Valid() bool {
	return s == SectionNone || s == SectionFavorite || s == SectionWork || s == SectionPersonal
}
func (s Section) Label() string {
	switch s {
	case SectionFavorite:
		return "⭐ favorites"
	case SectionWork:
		return "💼 work"
	case SectionPersonal:
		return "🏠 personal"
	}
	return "spaces"
}
func (w *Workspace) SidebarSection() Section {
	if w.Section == "" {
		return SectionNone
	}
	return w.Section
}

type CmdSetSection struct {
	Index   int
	Section Section
}
type CmdToggleSection struct{ Section Section }
type CmdSidebarPreset struct{ Preset string }
type CmdDuplicateWorkspace struct {
	Index int
	Focus bool
	Cwds  map[int]string
}

func (CmdSetSection) isCommand()         {}
func (CmdToggleSection) isCommand()      {}
func (CmdSidebarPreset) isCommand()      {}
func (CmdDuplicateWorkspace) isCommand() {}
func (s *AppState) SidebarBounds() (int, int, int) {
	lo, hi, normal := s.SidebarMin, s.SidebarMax, s.SidebarDefault
	if lo <= 0 {
		lo = SidebarMinDefault
	}
	if hi <= 0 {
		hi = SidebarMaxDefault
	}
	if lo > hi {
		lo, hi = SidebarMinDefault, SidebarMaxDefault
	}
	if normal <= 0 {
		normal = SidebarNormalDefault
	}
	return lo, hi, max(lo, min(hi, normal))
}
func (s *AppState) ExpandedSidebarWidth() int {
	if s.SidebarSize <= 0 {
		return SidebarWidth
	}
	lo, hi, _ := s.SidebarBounds()
	return max(lo, min(hi, s.SidebarSize))
}
func (s *AppState) SetSidebarPreset(p string) {
	lo, hi, n := s.SidebarBounds()
	switch p {
	case "narrow":
		s.SidebarSize = lo
		s.SidebarManual = true
	case "normal":
		s.SidebarSize = n
		s.SidebarManual = false
	case "wide":
		s.SidebarSize = hi
		s.SidebarManual = true
	}
}
func (s *AppState) PresetLabel() string {
	lo, _, n := s.SidebarBounds()
	w := s.ExpandedSidebarWidth()
	if w <= lo {
		return " NARROW "
	}
	if w <= n {
		return " NORMAL "
	}
	return " WIDE "
}
func (s *AppState) CyclePreset() string {
	lo, _, n := s.SidebarBounds()
	w := s.ExpandedSidebarWidth()
	if w <= lo {
		return "normal"
	}
	if w <= n {
		return "wide"
	}
	return "narrow"
}
func (s *AppState) VisibleWorkspaceOrder() []int {
	var out []int
	for _, sec := range SectionOrder {
		if s.CollapsedSections[sec] {
			continue
		}
		for i, w := range s.Workspaces {
			if w.SidebarSection() == sec && !s.WorktreeHidden(i) {
				out = append(out, i)
			}
		}
	}
	return out
}
func (s *AppState) WorktreeHidden(i int) bool {
	child := s.Workspaces[i].Worktree
	if child == nil || !child.IsLinkedWorktree {
		return false
	}
	for _, w := range s.Workspaces {
		if w.Worktree != nil && !w.Worktree.IsLinkedWorktree && w.Worktree.RepoKey == child.RepoKey {
			return w.GroupCollapsed
		}
	}
	return false
}
func (s *AppState) duplicateWorkspace(c CmdDuplicateWorkspace) []Effect {
	if c.Index < 0 || c.Index >= len(s.Workspaces) {
		return nil
	}
	src := s.Workspaces[c.Index]
	s.nextWorkspaceID++
	ws := &Workspace{ID: s.nextWorkspaceID, Name: src.Name, Cwd: src.Cwd, CustomName: src.CustomName, Section: src.Section, ActiveTab: src.ActiveTab}
	var eff []Effect
	for _, t := range src.Tabs {
		ids := map[int]int{}
		var order []int
		for _, old := range t.Layout.PaneIDs() {
			id := s.allocPane()
			ids[old] = id
			order = append(order, id)
			cwd := c.Cwds[old]
			if cwd == "" {
				cwd = src.Cwd
			}
			eff = append(eff, EffSpawnPane{Pane: id, Cwd: cwd})
			if label := s.PaneLabels[old]; label != "" {
				if s.PaneLabels == nil {
					s.PaneLabels = map[int]string{}
				}
				s.PaneLabels[id] = label
			}
		}
		var clone func(*layout.Node) *layout.Node
		clone = func(n *layout.Node) *layout.Node {
			if n.Leaf >= 0 {
				return layout.LeafNode(ids[n.Leaf])
			}
			return layout.SplitNode(n.Direction, n.Ratio, clone(n.First), clone(n.Second))
		}
		s.nextTabID++
		ws.Tabs = append(ws.Tabs, &Tab{ID: s.nextTabID, Name: t.Name, Layout: layout.Restore(clone(t.Layout.Root()), ids[t.Layout.Focused()], order)})
	}
	s.Workspaces = append(s.Workspaces, ws)
	if c.Focus {
		s.Active = len(s.Workspaces) - 1
	}
	return append(eff, EffSyncLayout{})
}
