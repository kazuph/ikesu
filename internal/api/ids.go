package api

import (
	"strconv"
	"strings"
)

// Public ID formats follow herdr:
//   panes:      p<n>       (accepts p<n>, p_<n>, %<n>, <n>)
//   workspaces: s<n>       (accepts s<n>, w<n> as alias, w_<i> and <i> as index)
//   tabs:       s<w>:t<n>  (accepts t_<wsref>_<n>, <wsref>:t<n>, <wsref>:<n>)

// FormatPaneID renders the public id for a pane.
func FormatPaneID(id int) string {
	return "p" + strconv.Itoa(id)
}

// FormatWorkspaceID renders the public id for a workspace.
func FormatWorkspaceID(id int) string {
	return "s" + strconv.Itoa(id)
}

// FormatTabID renders the public id for a tab inside a workspace.
func FormatTabID(workspaceID, number int) string {
	return FormatWorkspaceID(workspaceID) + ":t" + strconv.Itoa(number)
}

// ParsePaneRef extracts the raw pane number from a public id spelling.
// The caller must check that a live pane owns the number.
func ParsePaneRef(s string) (int, bool) {
	if rest, ok := strings.CutPrefix(s, "p_"); ok {
		s = rest
	} else if rest, ok := strings.CutPrefix(s, "%"); ok {
		s = rest
	} else if rest, ok := strings.CutPrefix(s, "p"); ok {
		s = rest
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// WorkspaceRef resolves to a workspace either by public id (Number > 0) or
// by 1-based position in the workspace list (Index > 0).
type WorkspaceRef struct {
	Number int
	Index  int
}

// ParseWorkspaceRef accepts "s<n>" (id), "w<n>" (legacy alias for s<n>),
// "w_<i>" or bare "<i>" (1-based position).
func ParseWorkspaceRef(s string) (WorkspaceRef, bool) {
	if rest, ok := strings.CutPrefix(s, "s"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return WorkspaceRef{Number: n}, true
		}
		return WorkspaceRef{}, false
	}
	if rest, ok := strings.CutPrefix(s, "w_"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return WorkspaceRef{Index: n}, true
		}
		return WorkspaceRef{}, false
	}
	if rest, ok := strings.CutPrefix(s, "w"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return WorkspaceRef{Number: n}, true
		}
		return WorkspaceRef{}, false
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return WorkspaceRef{Index: n}, true
	}
	return WorkspaceRef{}, false
}

// TabRef resolves to a tab by workspace reference plus either the tab's
// public number (Number > 0) or its 1-based position in the workspace
// (Index > 0).
type TabRef struct {
	Workspace WorkspaceRef
	Number    int
	Index     int
}

// ParseTabRef accepts "t_<wsref>_<n>" (positional), "<wsref>:t<n>"
// (by tab number), or "<wsref>:<n>" (positional).
func ParseTabRef(s string) (TabRef, bool) {
	if rest, ok := strings.CutPrefix(s, "t_"); ok {
		wsRaw, tabRaw, ok := cutLast(rest, '_')
		if !ok {
			return TabRef{}, false
		}
		ws, ok := ParseWorkspaceRef(wsRaw)
		if !ok {
			return TabRef{}, false
		}
		n, err := strconv.Atoi(tabRaw)
		if err != nil || n <= 0 {
			return TabRef{}, false
		}
		return TabRef{Workspace: ws, Index: n}, true
	}
	wsRaw, tabRaw, ok := cutLast(s, ':')
	if !ok {
		return TabRef{}, false
	}
	ws, ok := ParseWorkspaceRef(wsRaw)
	if !ok {
		return TabRef{}, false
	}
	if num, ok := strings.CutPrefix(tabRaw, "t"); ok {
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			return TabRef{Workspace: ws, Number: n}, true
		}
		return TabRef{}, false
	}
	if n, err := strconv.Atoi(tabRaw); err == nil && n > 0 {
		return TabRef{Workspace: ws, Index: n}, true
	}
	return TabRef{}, false
}

func cutLast(s string, sep byte) (string, string, bool) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
