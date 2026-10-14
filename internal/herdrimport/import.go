package herdrimport

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/session"
)

// Plan is the immutable preview for one source snapshot. Digest prevents
// confirmation of one snapshot from silently importing a different one.
type Plan struct {
	File       *session.File `json:"file"`
	Digest     string        `json:"digest"`
	Running    bool          `json:"herdr_running"`
	Panes      []PaneInfo    `json:"panes"`
	Unrestored []string      `json:"unrestored"`
}
type PaneInfo struct {
	Workspace string `json:"workspace"`
	Tab       string `json:"tab"`
	Number    int    `json:"pane"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd"`
	Agent     string `json:"agent,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func Load(dir string, cfg config.Config) (*Plan, error) {
	var src saved
	var led ledger
	var snap snapshot
	h := sha256.New()
	for _, item := range []struct {
		name     string
		dst      any
		optional bool
	}{{"session.json", &src, false}, {"agent-session-ledger.json", &led, true}, {"agent-session-snapshot.json", &snap, true}} {
		data, err := read(dir, item.name, item.dst, item.optional)
		if err != nil {
			return nil, err
		}
		h.Write([]byte(item.name))
		h.Write(data)
	}
	if src.Version != 5 {
		return nil, fmt.Errorf("unsupported herdr session version %d (want 5)", src.Version)
	}
	if len(src.Workspaces) == 0 || src.Active == nil || *src.Active < 0 || *src.Active >= len(src.Workspaces) || src.Selected == nil || *src.Selected < 0 || *src.Selected >= len(src.Workspaces) {
		return nil, fmt.Errorf("invalid workspace focus or empty session")
	}
	if src.SidebarWidth == nil || *src.SidebarWidth <= 0 {
		return nil, fmt.Errorf("invalid sidebar width")
	}
	running, err := Running(dir)
	if err != nil {
		return nil, err
	}
	p := &Plan{Digest: fmt.Sprintf("%x", h.Sum(nil)), Running: running, File: &session.File{Version: session.CurrentVersion, Active: *src.Active, SidebarWidth: *src.SidebarWidth, SidebarManual: true, SidebarVisible: true, CollapsedSections: map[string]bool{}}}
	// These herdr-only display preferences have no gtx counterpart. They
	// are explicitly disclosed, never mapped to a similar unrelated value.
	if src.SidebarSectionSplit != 0 {
		p.Unrestored = append(p.Unrestored, "sidebar_section_split: gtx はセクションの高さ比率を保存しない")
	}
	if *src.Selected != *src.Active {
		p.Unrestored = append(p.Unrestored, "selected: gtx は sidebar 選択を独立して保存しない（active のフォーカスを復元）")
	}
	if len(src.CollapsedSpaceKeys) > 0 {
		return nil, fmt.Errorf("unsupported collapsed_space_keys")
	}
	for _, s := range src.CollapsedSections {
		if !validSection(s) {
			return nil, fmt.Errorf("unsupported collapsed section %q", s)
		}
		p.File.CollapsedSections[s] = true
	}
	wsIDs := map[string]bool{}
	paneIDs := map[int]bool{}
	for _, w := range src.Workspaces {
		if w.ID == "" || wsIDs[w.ID] || !validSection(w.Section) || len(w.Tabs) == 0 || w.ActiveTab == nil || *w.ActiveTab < 0 || *w.ActiveTab >= len(w.Tabs) {
			return nil, fmt.Errorf("invalid workspace %q", w.ID)
		}
		wsIDs[w.ID] = true
		if err := checkDir(w.Cwd); err != nil {
			return nil, err
		}
		if len(w.TabNumbers) != len(w.Tabs) {
			return nil, fmt.Errorf("workspace %s: invalid public tab numbers", w.ID)
		}
		name := filepath.Base(w.Cwd)
		custom := w.CustomName != nil && *w.CustomName != ""
		if custom {
			name = *w.CustomName
		}
		sw := session.Workspace{ID: len(p.File.Workspaces) + 1, Name: name, Cwd: w.Cwd, Section: w.Section, CustomName: custom, DefaultName: !custom, ActiveTab: *w.ActiveTab}
		tabNums := map[int]bool{}
		for ti, t := range w.Tabs {
			if w.TabNumbers[ti] <= 0 || tabNums[w.TabNumbers[ti]] || w.NextTabNumber <= w.TabNumbers[ti] {
				return nil, fmt.Errorf("workspace %s: invalid tab numbering", w.ID)
			}
			tabNums[w.TabNumbers[ti]] = true

			n, leaves, err := convertTree(&t.Layout)
			if err != nil {
				return nil, err
			}
			if len(leaves) != len(t.Panes) || len(t.Order) != len(leaves) {
				return nil, fmt.Errorf("workspace %s tab %d: pane/layout mismatch", w.ID, ti+1)
			}
			leafSet := map[int]bool{}
			for _, id := range leaves {
				if leafSet[id] || paneIDs[id] {
					return nil, fmt.Errorf("duplicate pane %d", id)
				}
				leafSet[id] = true
				paneIDs[id] = true
			}
			if !leafSet[t.Focused] || !leafSet[t.RootPane] {
				return nil, fmt.Errorf("invalid pane focus/root")
			}
			orderSet := map[int]bool{}
			for _, id := range t.Order {
				if !leafSet[id] || orderSet[id] {
					return nil, fmt.Errorf("invalid pane order")
				}
				orderSet[id] = true
			}
			p.File.NextTabID++
			st := session.Tab{ID: p.File.NextTabID, Focused: t.Focused, Order: t.Order, Layout: n}
			if t.Zoomed {
				st.Zoomed = t.Focused
			}
			if t.CustomName != nil {
				st.Name = *t.CustomName
			}
			for _, id := range t.Order {
				sp, ok := t.Panes[strconv.Itoa(id)]
				if !ok || sp.Number != id {
					return nil, fmt.Errorf("pane %d: missing/mismatched pane record", id)
				}
				if err := checkDir(sp.Cwd); err != nil {
					return nil, err
				}
				rp := session.Pane{ID: id, Cwd: sp.Cwd, Label: sp.Label, Title: sp.Title}
				info := PaneInfo{Workspace: name, Tab: st.Name, Number: id, Label: sp.Label, Cwd: sp.Cwd}
				r, reason := resolveRef(w, ti, id, sp, led, snap)
				if r != nil {
					info.Agent, info.SessionID = r.Agent, r.Value
					switch {
					case reason != "":
					case running:
						reason = "herdr が稼働中：会話を再開せず shell として復元"
					case r.Kind != "id" && r.Kind != "path":
						reason = "未対応の会話参照形式"
					case !session.SafeSessionValue(r.Kind, r.Value):
						reason = "会話 ID 不明または不正"
					default:
						if _, ok := session.ResumeCommand(cfg, r.Agent, r.Kind, r.Value); !ok {
							reason = "未対応の agent 再開コマンド"
						} else {
							rp.Agent, rp.SessionKind, rp.SessionID = r.Agent, r.Kind, r.Value
						}
					}
				}
				info.Reason = reason
				p.Panes = append(p.Panes, info)
				st.Panes = append(st.Panes, rp)
				if id > p.File.NextPaneID {
					p.File.NextPaneID = id
				}
			}
			sw.Tabs = append(sw.Tabs, st)
		}
		p.File.Workspaces = append(p.File.Workspaces, sw)
	}
	p.File.NextWorkspaceID = len(p.File.Workspaces)
	// The same recorded conversation in two panes cannot be restarted twice.
	counts := map[string]int{}
	p.File.EachPane(func(_ *session.Workspace, _ *session.Tab, x *session.Pane) {
		if x.SessionID != "" {
			counts[x.Agent+":"+x.SessionID]++
		}
	})
	p.File.EachPane(func(_ *session.Workspace, _ *session.Tab, x *session.Pane) {
		if x.SessionID != "" && counts[x.Agent+":"+x.SessionID] > 1 {
			for i := range p.Panes {
				if p.Panes[i].Number == x.ID {
					p.Panes[i].Reason = "会話 ID が複数 pane に重複：shell として復元"
				}
			}
			x.Agent = ""
			x.SessionID = ""
			x.SessionKind = ""
		}
	})
	return p, nil
}
func validSection(s string) bool {
	return s == "none" || s == "favorite" || s == "work" || s == "personal"
}
func checkDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("unsupported non-absolute cwd %q", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cwd %q: %w", path, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("cwd %q is not a directory", path)
	}
	return nil
}
func convertTree(t *tree) (*session.Node, []int, error) {
	if t == nil || (t.Pane == nil) == (t.Split == nil) {
		return nil, nil, fmt.Errorf("invalid Pane/Split layout")
	}
	if t.Pane != nil {
		if *t.Pane <= 0 {
			return nil, nil, fmt.Errorf("invalid pane number")
		}
		return &session.Node{Leaf: *t.Pane}, []int{*t.Pane}, nil
	}
	s := t.Split
	if s.Ratio == nil || *s.Ratio <= 0 || *s.Ratio >= 1 {
		return nil, nil, fmt.Errorf("invalid split ratio")
	}
	dir := ""
	switch s.Direction {
	case "Vertical":
		dir = "v"
	case "Horizontal":
		dir = "h"
	default:
		return nil, nil, fmt.Errorf("unsupported split direction %q", s.Direction)
	}
	a, ai, err := convertTree(s.First)
	if err != nil {
		return nil, nil, err
	}
	b, bi, err := convertTree(s.Second)
	if err != nil {
		return nil, nil, err
	}
	return &session.Node{Leaf: -1, Direction: dir, Ratio: *s.Ratio, First: a, Second: b}, append(ai, bi...), nil
}
