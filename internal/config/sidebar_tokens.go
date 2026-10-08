package config

import "fmt"

type SidebarToken struct {
	Token     string
	Fg        *string
	Bold, Dim *bool
}

func (t *SidebarToken) UnmarshalTOML(v any) error {
	if s, ok := v.(string); ok {
		t.Token = s
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("sidebar token must be a string or table")
	}
	s, ok := m["token"].(string)
	if !ok {
		return fmt.Errorf("sidebar token requires token string")
	}
	t.Token = s
	if v, exists := m["fg"]; exists {
		s, ok := v.(string)
		if !ok || !validTokenColor(s) {
			return fmt.Errorf("sidebar token fg requires #RGB or #RRGGBB")
		}
		t.Fg = &s
	}
	for name, dest := range map[string]**bool{"bold": &t.Bold, "dim": &t.Dim} {
		if v, exists := m[name]; exists {
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("sidebar token %s requires boolean", name)
			}
			*dest = &b
		}
	}
	return nil
}
func validTokenColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

type SidebarPanel struct {
	Rows        [][]SidebarToken            `toml:"rows"`
	RowGap      int                         `toml:"row_gap"`
	RowsByAgent map[string][][]SidebarToken `toml:"rows_by_agent"`
}
type SidebarPanels struct {
	Agents SidebarPanel `toml:"agents"`
	Spaces SidebarPanel `toml:"spaces"`
}

func (c Config) ValidateSidebar() error {
	check := func(panel SidebarPanel, agent bool) error {
		if panel.RowGap < 0 {
			return fmt.Errorf("sidebar row_gap must not be negative")
		}
		rows := append([][]SidebarToken(nil), panel.Rows...)
		for _, specific := range panel.RowsByAgent {
			rows = append(rows, specific...)
		}
		for _, row := range rows {
			for _, t := range row {
				switch t.Token {
				case "state_icon", "state_text", "workspace":
				case "pane", "tab", "agent", "terminal_title":
					if !agent {
						return fmt.Errorf("unsupported space token: %s", t.Token)
					}
				case "branch", "git_status":
					if agent {
						return fmt.Errorf("unsupported agent token: %s", t.Token)
					}
				case "terminal_title_stripped":
					return fmt.Errorf("unsupported token terminal_title_stripped: stripping contract not available")
				default:
					if len(t.Token) > 1 && t.Token[0] == '$' && !agent {
						continue
					}
					return fmt.Errorf("unsupported sidebar token: %s", t.Token)
				}
			}
		}
		return nil
	}
	if err := check(c.UI.Sidebar.Agents, true); err != nil {
		return err
	}
	return check(c.UI.Sidebar.Spaces, false)
}
