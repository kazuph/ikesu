// Package session defines the session.json schema a ikesu server writes
// under $IKESU_STATE_DIR/<session>/ and restores on startup (SPEC G3/G5).
// The file is pure data: workspaces, tabs, split trees, per-pane working
// directories, stable pane numbers, pane labels, and each pane's agent
// record (agent kind, session id, title). Writes are atomic — a temp file
// is renamed over the target so a crash never leaves a corrupt file.
package session

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"os"
	"path/filepath"
	"time"

	"github.com/kazuph/ikesu/internal/layout"
)

// CurrentVersion is the session.json schema version.
const CurrentVersion = 1

// File is the whole persisted session.
type File struct {
	Version           int             `json:"version"`
	SavedAt           string          `json:"saved_at"`
	Active            int             `json:"active"` // active workspace index
	SidebarWidth      int             `json:"sidebar_width,omitempty"`
	SidebarManual     bool            `json:"sidebar_manual,omitempty"`
	CollapsedSections map[string]bool `json:"collapsed_sections,omitempty"`
	SidebarVisible    bool            `json:"sidebar_visible"`
	NextPaneID        int             `json:"next_pane_id"`
	NextWorkspaceID   int             `json:"next_workspace_id"`
	NextTabID         int             `json:"next_tab_id"`
	Workspaces        []Workspace     `json:"workspaces"`
}

// Workspace is one saved workspace.
type Workspace struct {
	Worktree       *api.WorkspaceWorktreeInfo `json:"worktree,omitempty"`
	Section        string                     `json:"section,omitempty"`
	CustomName     bool                       `json:"custom_name,omitempty"`
	DefaultName    bool                       `json:"default_name,omitempty"`
	GroupCollapsed bool                       `json:"group_collapsed,omitempty"`
	ID             int                        `json:"id"`
	Name           string                     `json:"name"`
	Cwd            string                     `json:"cwd"`
	ActiveTab      int                        `json:"active_tab"`
	Tabs           []Tab                      `json:"tabs"`
}

// Tab is one saved tab with its split tree and pane records.
type Tab struct {
	Zoomed  int    `json:"zoomed,omitempty"` // pane id; absent/zero means no zoom
	ID      int    `json:"id"`
	Name    string `json:"name,omitempty"`
	Focused int    `json:"focused"` // pane id
	Order   []int  `json:"order"`   // pane ids in creation order
	Layout  *Node  `json:"layout"`
	Panes   []Pane `json:"panes"`
}

// Node is one serialized BSP node mirroring layout.Node: Leaf > 0 is a
// pane id; otherwise the node is a split with Direction "v" or "h".
type Node struct {
	Leaf      int     `json:"leaf"`
	Direction string  `json:"direction,omitempty"` // "v" | "h" on splits
	Ratio     float64 `json:"ratio,omitempty"`
	First     *Node   `json:"first,omitempty"`
	Second    *Node   `json:"second,omitempty"`
}

// Pane is the persisted record of one pane.
type Pane struct {
	SessionStartSource string `json:"session_start_source,omitempty"`
	ID                 int    `json:"id"`
	Cwd                string `json:"cwd"`
	Label              string `json:"label,omitempty"`
	Agent              string `json:"agent,omitempty"`        // canonical agent id (claude, codex, ...)
	SessionID          string `json:"session_id,omitempty"`   // exact session ref
	SessionKind        string `json:"session_kind,omitempty"` // "id" | "path"
	Title              string `json:"title,omitempty"`
}

// Load reads path. A missing file returns (nil, nil). A corrupt file
// returns an error; callers treat that as "no session" and start fresh —
// restoring must fail closed, never guess (SPEC G3).
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Version != CurrentVersion {
		return nil, fmt.Errorf("%s: unsupported version %d", path, f.Version)
	}
	return &f, nil
}

// Save writes f to path atomically: marshal → temp file → fsync → rename.
func Save(path string, f *File) error {
	f.Version = CurrentVersion
	f.SavedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	w, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Sync(); err != nil {
		w.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// FromLayoutNode converts a layout tree to its serialized form.
func FromLayoutNode(n *layout.Node) *Node {
	if n == nil {
		return nil
	}
	if n.Leaf >= 0 {
		return &Node{Leaf: n.Leaf}
	}
	dir := "v"
	if n.Direction == layout.Horizontal {
		dir = "h"
	}
	return &Node{
		Leaf:      -1,
		Direction: dir,
		Ratio:     n.Ratio,
		First:     FromLayoutNode(n.First),
		Second:    FromLayoutNode(n.Second),
	}
}

// ToLayoutNode converts a serialized node back to a layout tree. It
// returns nil for malformed nodes; callers validate beforehand.
func ToLayoutNode(n *Node) *layout.Node {
	if n == nil {
		return nil
	}
	if n.Leaf >= 0 {
		return layout.LeafNode(n.Leaf)
	}
	if n.First == nil || n.Second == nil {
		return nil
	}
	dir := layout.Vertical
	if n.Direction == "h" {
		dir = layout.Horizontal
	}
	first := ToLayoutNode(n.First)
	second := ToLayoutNode(n.Second)
	if first == nil || second == nil {
		return nil
	}
	return layout.SplitNode(dir, n.Ratio, first, second)
}

// EachPane calls fn for every pane record; fn may mutate the record.
func (f *File) EachPane(fn func(w *Workspace, t *Tab, p *Pane)) {
	for wi := range f.Workspaces {
		for ti := range f.Workspaces[wi].Tabs {
			for pi := range f.Workspaces[wi].Tabs[ti].Panes {
				fn(&f.Workspaces[wi], &f.Workspaces[wi].Tabs[ti], &f.Workspaces[wi].Tabs[ti].Panes[pi])
			}
		}
	}
}

// Validate enforces the G5 pane-numbering rules: every pane id must be
// positive and unique, every layout leaf must have a pane record, focus
// and order must reference real leaves, and ids must not be missing. On
// any violation the whole file is renumbered — partial adoption of saved
// ids is forbidden — and a warning is recorded. Tabs with no panes and
// workspaces with no tabs are dropped (a state with no workspaces returns
// a warning; the caller starts fresh).
func (f *File) Validate() (warnings []string) {
	// numberingBad flags the G5 violations that force a whole-file
	// renumber: missing/zero/duplicate pane ids, or a mismatch between
	// layout leaves and pane records.
	numberingBad := false
	// Structural pass: normalize trees, drop empty tabs/workspaces.
	var wsOut []Workspace
	for wi := range f.Workspaces {
		w := &f.Workspaces[wi]
		var tabsOut []Tab
		for ti := range w.Tabs {
			t := &w.Tabs[ti]
			leaves, ok := validTree(t.Layout)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("tab %d of workspace %d has a broken layout; dropped", t.ID, w.ID))
				continue
			}
			// Pane records keyed by id. A record missing from the layout,
			// or a leaf with no record, is a layout inconsistency.
			recs := map[int]int{} // record id → reference count
			for i := range t.Panes {
				recs[t.Panes[i].ID]++
			}
			var kept []Pane
			byID := map[int]*Pane{}
			for i := range t.Panes {
				byID[t.Panes[i].ID] = &t.Panes[i]
			}
			for _, id := range leaves {
				if recs[id] == 0 {
					kept = append(kept, Pane{ID: id})
					numberingBad = true
				} else {
					kept = append(kept, *byID[id])
				}
			}
			for id := range recs {
				if !contains(leaves, id) {
					numberingBad = true
				}
			}
			t.Panes = kept
			if t.Zoomed != 0 && !contains(leaves, t.Zoomed) {
				warnings = append(warnings, fmt.Sprintf("tab %d has an invalid zoom pane; zoom cleared", t.ID))
				t.Zoomed = 0
			}
			if !contains(leaves, t.Focused) {
				t.Focused = leaves[0]
			}
			var ord []int
			seenOrd := map[int]bool{}
			for _, id := range t.Order {
				if contains(leaves, id) && !seenOrd[id] {
					ord = append(ord, id)
					seenOrd[id] = true
				}
			}
			for _, id := range leaves {
				if !seenOrd[id] {
					ord = append(ord, id)
				}
			}
			t.Order = ord
			tabsOut = append(tabsOut, *t)
		}
		w.Tabs = tabsOut
		if len(w.Tabs) == 0 {
			warnings = append(warnings, fmt.Sprintf("workspace %d had no usable tabs; dropped", w.ID))
			continue
		}
		if w.ActiveTab < 0 || w.ActiveTab >= len(w.Tabs) {
			w.ActiveTab = 0
		}
		wsOut = append(wsOut, *w)
	}
	f.Workspaces = wsOut

	// G5 numbering pass: pane ids must be >0 and unique.
	seen := map[int]bool{}
	f.EachPane(func(_ *Workspace, _ *Tab, p *Pane) {
		if p.ID <= 0 || seen[p.ID] {
			numberingBad = true
		}
		seen[p.ID] = true
	})
	if numberingBad {
		warnings = append(warnings, "saved pane numbers are missing, zero, duplicated, or inconsistent with the layouts; renumbered all panes")
		f.renumber()
	}
	// Workspace and tab ids get the same uniqueness treatment; they are
	// display-only so a renumber is silent.
	f.rekey()
	if f.Active < 0 || f.Active >= len(f.Workspaces) {
		f.Active = 0
	}
	return warnings
}

// renumber assigns fresh sequential pane ids and rewrites every reference
// (leaves, focus, order). The structural pass already rebuilt each tab's
// Panes in leaf order, so the i-th leaf positionally receives the i-th
// record's id — duplicated or zero old ids cannot collide that way.
func (f *File) renumber() {
	zoomPositions := map[*Tab]int{}
	for wi := range f.Workspaces {
		for ti := range f.Workspaces[wi].Tabs {
			t := &f.Workspaces[wi].Tabs[ti]
			if t.Zoomed == 0 {
				continue
			}
			pos, count := 0, 0
			for i, p := range t.Panes {
				if p.ID == t.Zoomed {
					pos = i
					count++
				}
			}
			if count == 1 {
				zoomPositions[t] = pos
			}
			t.Zoomed = 0
		}
	}
	next := 0
	f.EachPane(func(_ *Workspace, _ *Tab, p *Pane) {
		next++
		p.ID = next
	})
	for wi := range f.Workspaces {
		for ti := range f.Workspaces[wi].Tabs {
			t := &f.Workspaces[wi].Tabs[ti]
			idx := 0
			var walk func(n *Node)
			walk = func(n *Node) {
				if n == nil {
					return
				}
				if n.Leaf >= 0 {
					if idx < len(t.Panes) {
						n.Leaf = t.Panes[idx].ID
					}
					idx++
					return
				}
				walk(n.First)
				walk(n.Second)
			}
			walk(t.Layout)
			if pos, ok := zoomPositions[t]; ok {
				t.Zoomed = t.Panes[pos].ID
			}
			t.Focused = t.Panes[0].ID
			t.Order = t.Order[:0]
			for _, p := range t.Panes {
				t.Order = append(t.Order, p.ID)
			}
		}
	}
	f.NextPaneID = next
}

// rekey makes workspace and tab ids unique (they are not required to be
// stable across restarts, only unambiguous within one snapshot).
func (f *File) rekey() {
	wseen := map[int]bool{}
	maxW := 0
	for wi := range f.Workspaces {
		w := &f.Workspaces[wi]
		if w.ID <= 0 || wseen[w.ID] {
			maxW++
			w.ID = maxW
		}
		wseen[w.ID] = true
		if w.ID > maxW {
			maxW = w.ID
		}
		tseen := map[int]bool{}
		maxT := 0
		for ti := range w.Tabs {
			t := &w.Tabs[ti]
			if t.ID <= 0 || tseen[t.ID] {
				maxT++
				t.ID = maxT
			}
			tseen[t.ID] = true
			if t.ID > maxT {
				maxT = t.ID
			}
		}
	}
	// Keep counters beyond every id actually in use.
	maxP := 0
	f.EachPane(func(_ *Workspace, _ *Tab, p *Pane) {
		if p.ID > maxP {
			maxP = p.ID
		}
	})
	if f.NextPaneID < maxP {
		f.NextPaneID = maxP
	}
	if f.NextWorkspaceID < maxW {
		f.NextWorkspaceID = maxW
	}
	for wi := range f.Workspaces {
		for ti := range f.Workspaces[wi].Tabs {
			if f.Workspaces[wi].Tabs[ti].ID > f.NextTabID {
				f.NextTabID = f.Workspaces[wi].Tabs[ti].ID
			}
		}
	}
}

// validTree checks that the serialized tree is well-formed and returns
// its leaf ids in traversal order.
func validTree(n *Node) ([]int, bool) {
	if n == nil {
		return nil, false
	}
	// Leaf >= 0 mirrors layout.Node's convention; a leaf id of 0 is kept
	// here so the G5 numbering pass — not this structural check — flags
	// it and renumbers the whole file.
	if n.Leaf >= 0 {
		return []int{n.Leaf}, true
	}
	if n.Direction != "v" && n.Direction != "h" {
		return nil, false
	}
	f, ok1 := validTree(n.First)
	s, ok2 := validTree(n.Second)
	if !ok1 || !ok2 {
		return nil, false
	}
	return append(f, s...), true
}

func contains(ids []int, id int) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// safeSessionIDByte reports whether c is in the spec's session-id set:
// ASCII alphanumerics plus - _ . only. Checked byte-wise so no compiled
// regexp is retained in the heap (SPEC P1 memory budget).
func safeSessionIDByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' ||
		'0' <= c && c <= '9' || c == '.' || c == '_' || c == '-'
}

// SafeSessionID reports whether v is a safe session id (SPEC G3): empty,
// over 128 chars, leading '-', the literals "last"/"--last", or any char
// outside [A-Za-z0-9._-] are rejected.
func SafeSessionID(v string) bool {
	if v == "" || len(v) > 128 || v[0] == '-' || v == "last" || v == "--last" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !safeSessionIDByte(v[i]) {
			return false
		}
	}
	return true
}

// SafeSessionPath reports whether v is acceptable as a transcript-path
// session ref (kind=path). The value is later typed into a pane, so any
// character that could smuggle shell syntax (whitespace, quotes,
// metacharacters) is rejected; paths that fail stay unresumable.
// SafeSessionValue dispatches on the ref kind: "path" values are
// transcript files, everything else is an id.
func SafeSessionValue(kind, v string) bool {
	if kind == "path" {
		return SafeSessionPath(v)
	}
	return SafeSessionID(v)
}

func SafeSessionPath(v string) bool {
	if v == "" || len(v) > 4096 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '.' || r == '_' || r == '-' || r == '~' || r == '+':
		default:
			return false
		}
	}
	return true
}

// SessionFileName is the file name inside a session directory.
const SessionFileName = "session.json"

// Path joins a session directory and the file name (kept here so callers
// never spell the name out).
func Path(sessionDir string) string {
	return filepath.Join(sessionDir, SessionFileName)
}
