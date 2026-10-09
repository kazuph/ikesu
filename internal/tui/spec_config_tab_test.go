package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestSPECExistingConfigHidesEveryTabHitAndKeepsBottomBar(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nshow_tab_bar=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	a.SetUIFlags(cfg.ShowTabBar(), cfg.HideTabBarWhenSingleTab(), cfg.PaneBorders())
	a.exec(model.CmdNewTab{Workspace: 0})
	out := render.Compose(render.Input{State: a.st})
	if len(a.st.ActiveWorkspace().Tabs) != 2 || a.st.TabBarArea().H != 0 || len(out.TabHits) != 0 || out.NewTabRect.W != 0 {
		t.Fatal("hidden tab hit areas", out)
	}
	if out.ActionBar.Area.Y != a.st.Rows-1 || out.ActionBar.Area.H != 1 {
		t.Fatal("bottom bar", out.ActionBar.Area)
	}
}
