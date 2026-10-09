package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) OpenPluginPane(workspace, target int, placement, direction, cwd string, argv []string, env map[string]string, focus bool) (int, error) {
	if placement == "overlay" {
		return a.OpenOverlay(target, cwd, argv, env)
	}
	if workspace < 0 || workspace >= len(a.st.Workspaces) {
		return 0, fmt.Errorf("workspace not found")
	}
	before := map[int]bool{}
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				before[pid] = true
			}
		}
	}
	a.lastSpawnError = nil
	switch placement {
	case "tab":
		a.exec(model.CmdNewTab{Workspace: workspace, Cwd: cwd, Argv: argv, Env: env, NoFocus: !focus})
	case "split", "zoomed":
		dir := layout.Vertical
		switch direction {
		case "", "right", "vertical":
		case "down", "horizontal":
			dir = layout.Horizontal
		default:
			return 0, fmt.Errorf("invalid split direction %q", direction)
		}
		a.exec(model.CmdSplitPane{Workspace: workspace, Pane: target, Dir: dir, Ratio: 0.5, Focus: focus || placement == "zoomed", Cwd: cwd, Argv: argv, Env: env})
	default:
		return 0, fmt.Errorf("unsupported pane placement: %s", placement)
	}
	pid := 0
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if !before[id] {
					pid = id
				}
			}
		}
	}
	if pid == 0 {
		return 0, fmt.Errorf("pane could not be created")
	}
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdClosePane{Pane: pid})
		return 0, err
	}
	if focus || placement == "zoomed" {
		a.exec(model.CmdFocusPane{Pane: pid})
	}
	if placement == "zoomed" {
		a.exec(model.CmdZoomPane{Pane: pid, Mode: "on"})
	}
	return pid, nil
}
