package render

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/detect"
)

func revealIMECursor(c config.Config, agent string) bool {
	if !c.Experimental.RevealHiddenCursorForCJKIME {
		return false
	}
	if len(c.Experimental.CJKIMEAgents) == 0 {
		return true
	}
	for _, name := range c.Experimental.CJKIMEAgents {
		id := detect.Identify(name, name)
		if id != "" && id == agent {
			return true
		}
	}
	return false
}

// DECSCUSR's six cursor styles are specified by the terminal protocol.
func imeCursorShape(name string) int {
	switch name {
	case "block":
		return 1
	case "underline":
		return 3
	case "steady_underline":
		return 4
	case "bar":
		return 5
	case "steady_bar":
		return 6
	default:
		return 2
	}
}
