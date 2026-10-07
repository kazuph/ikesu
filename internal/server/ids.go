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
