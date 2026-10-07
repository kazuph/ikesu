package server

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePaneID accepts "p3", "%3", "p_3", or "3" and returns the int id.
func parsePaneID(s string) (int, bool) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(v, "%")
	v = strings.TrimPrefix(v, "p_")
	v = strings.TrimPrefix(v, "p")
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// parseWorkspaceID accepts "w3" or "3".
func parseWorkspaceID(s string) (int, bool) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(v, "w_")
	v = strings.TrimPrefix(v, "w")
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// fmtPaneID renders the public pane id ("p3").
func fmtPaneID(id int) string { return fmt.Sprintf("p%d", id) }

// fmtWorkspaceID renders the public workspace id ("w3").
func fmtWorkspaceID(id int) string { return fmt.Sprintf("w%d", id) }

// fmtTabID renders the public tab id ("w2:t3").
func fmtTabID(wsID, tabID int) string { return fmt.Sprintf("w%d:t%d", wsID, tabID) }

// parseTabID accepts "w2:t3" and returns the workspace and tab ids.
func parseTabID(s string) (wsID, tabID int, ok bool) {
	v := strings.TrimSpace(s)
	ws, rest, found := strings.Cut(v, ":")
	if !found {
		return 0, 0, false
	}
	tab := strings.TrimPrefix(rest, "t")
	wi, wok := parseWorkspaceID(ws)
	tn, err := strconv.Atoi(tab)
	if !wok || err != nil || tn <= 0 {
		return 0, 0, false
	}
	return wi, tn, true
}
