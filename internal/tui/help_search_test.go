package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func helpBind(a *App, cmd string) string {
	for _, row := range a.helpRows() {
		if row.Cmd == cmd {
			return row.Bind
		}
	}
	return "<missing>"
}

func TestHelpListsWorkspaceSearchKeys(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 120, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	if got := helpBind(a, "workspace search"); got != "prefix+/" {
		t.Fatalf("default %q", got)
	}
	for toml, want := range map[string]string{"[keys]\nworkspace_search='cmd+f'\n": "cmd+f / prefix+/", "[keys]\nworkspace_search=''\n": "unset"} {
		cfg, err := config.Decode([]byte(toml))
		if err != nil {
			t.Fatal(err)
		}
		a.sidebarConfig = cfg
		if got := helpBind(a, "workspace search"); !strings.EqualFold(got, want) {
			t.Fatalf("%q -> %q want %q", toml, got, want)
		}
	}
}
