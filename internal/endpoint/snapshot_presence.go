package endpoint

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/render"
)

// Gob omits pointer fields whose scalar value is zero. Preserve presence
// separately so a first-row drop or explicitly disabled setting stays exact.
type snapshotPresence struct {
	Selected, Drop          bool
	ConfigBools, ConfigInts uint64
	Tokens                  []tokenPresence
}

type tokenPresence struct {
	Agents      bool
	Specific    bool
	Agent       string
	Row, Column int
	Bold, Dim   bool
}

func configPointers(c *config.Config) ([18]**bool, [8]**int) {
	return [18]**bool{
		&c.Onboarding, &c.Remote.ManageSSHConfig, &c.Update.VersionCheck, &c.Update.ManifestCheck,
		&c.UI.MouseCapture, &c.UI.CopyOnSelect, &c.UI.RedrawOnFocusGained, &c.UI.ConfirmClose,
		&c.UI.PromptNewTabName, &c.UI.PaneGaps, &c.UI.Sound.Enabled, &c.UI.Toast.Clipboard.Enabled,
		&c.UI.Toast.Enabled, &c.UI.ShowTabBar, &c.UI.HideTabBarWhenSingleTab, &c.UI.PaneBorders,
		&c.Session.ResumeAgentsOnRestore, &c.AgentRestore.Enabled,
	}, [8]**int{
		&c.Advanced.ScrollbackLimitBytes, &c.UI.MouseScrollLines, &c.UI.MobileWidthThreshold,
		&c.UI.SidebarWidth, &c.UI.SidebarMinWidth, &c.UI.SidebarMaxWidth,
		&c.UI.Toast.DelaySeconds, &c.AgentRestore.RestoreDelayMs,
	}
}

func optionalPresence(in render.Input) snapshotPresence {
	p := snapshotPresence{Selected: in.SelectedWorkspace != nil, Drop: in.DropIndex != nil}
	bools, ints := configPointers(&in.SidebarConfig)
	for i, field := range bools {
		if *field != nil && !**field {
			p.ConfigBools |= 1 << i
		}
	}
	for i, field := range ints {
		if *field != nil && **field == 0 {
			p.ConfigInts |= 1 << i
		}
	}
	for index, panel := range []config.SidebarPanel{in.SidebarConfig.UI.Sidebar.Spaces, in.SidebarConfig.UI.Sidebar.Agents} {
		capture := func(agent string, specific bool, rows [][]config.SidebarToken) {
			for y, row := range rows {
				for x, token := range row {
					bold, dim := token.Bold != nil && !*token.Bold, token.Dim != nil && !*token.Dim
					if bold || dim {
						p.Tokens = append(p.Tokens, tokenPresence{index == 1, specific, agent, y, x, bold, dim})
					}
				}
			}
		}
		capture("", false, panel.Rows)
		for agent, rows := range panel.RowsByAgent {
			capture(agent, true, rows)
		}
	}
	return p
}

func (p snapshotPresence) restore(in *render.Input) error {
	if p.Selected && in.SelectedWorkspace == nil {
		in.SelectedWorkspace = new(int)
	}
	if p.Drop && in.DropIndex == nil {
		in.DropIndex = new(int)
	}
	bools, ints := configPointers(&in.SidebarConfig)
	for i, field := range bools {
		if p.ConfigBools&(1<<i) != 0 {
			*field = new(bool)
		}
	}
	for i, field := range ints {
		if p.ConfigInts&(1<<i) != 0 {
			*field = new(int)
		}
	}
	for _, token := range p.Tokens {
		panel := &in.SidebarConfig.UI.Sidebar.Spaces
		if token.Agents {
			panel = &in.SidebarConfig.UI.Sidebar.Agents
		}
		rows := panel.Rows
		if token.Specific {
			rows = panel.RowsByAgent[token.Agent]
		}
		if token.Row < 0 || token.Row >= len(rows) || token.Column < 0 || token.Column >= len(rows[token.Row]) {
			return fmt.Errorf("invalid sidebar token presence position")
		}
		value := &rows[token.Row][token.Column]
		if token.Bold {
			value.Bold = new(bool)
		}
		if token.Dim {
			value.Dim = new(bool)
		}
	}
	return nil
}
