package config

import (
	"github.com/BurntSushi/toml"
	"testing"
)

func TestG2SidebarTokenStringInlineAndExplicitFalse(t *testing.T) {
	var c Config
	_, e := toml.Decode(`[ui.sidebar.agents]
rows = [["state_icon", {token="workspace",fg="#abc",bold=false,dim=true},"tab"]]
[ui.sidebar.spaces]
rows = [["workspace",{token="$custom",fg="#89b4fa",bold=true}],["git_status","branch"]]
`, &c)
	if e != nil {
		t.Fatal(e)
	}
	tokens := c.UI.Sidebar.Agents.Rows[0]
	if len(tokens) != 3 || tokens[0].Token != "state_icon" || tokens[0].Fg != nil || tokens[1].Bold == nil || *tokens[1].Bold || !*tokens[1].Dim {
		t.Fatal(tokens)
	}
	if e = c.ValidateSidebar(); e != nil {
		t.Fatal(e)
	}
	for _, fg := range []string{"red", "#12345", "#xyz"} {
		var bad Config
		if _, e := toml.Decode(`[ui.sidebar.agents]
rows = [[{token="workspace",fg="`+fg+`"}]]`, &bad); e == nil {
			t.Fatal("bad color accepted", fg)
		}
	}
}
func TestG2UnsupportedTokenExplicitError(t *testing.T) {
	for _, name := range []string{"terminal_title_stripped", "$pane_custom", "made-up"} {
		var c Config
		c.UI.Sidebar.Agents.Rows = [][]SidebarToken{{{Token: name}}}
		if c.ValidateSidebar() == nil {
			t.Fatal("unsupported approximation", name)
		}
	}
}
