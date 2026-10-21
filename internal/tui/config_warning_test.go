package tui

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestStartupConfigWarningsMatchSessionAndMonolith(t *testing.T) {
	teststate.Lock(t)
	for _, source := range []string{"[ui]\nsidebar_width='bad'\n", "[keys]\nnew_tab='prefix+zz'\n", "[keys]\nprefix=5\n", "[update]\nversion_check=true\n"} {
		c, err := config.Decode([]byte(source))
		if err != nil || len(c.Warnings) != 1 {
			t.Fatal(c.Warnings, err)
		}
		for _, server := range []bool{false, true} {
			a := specRuntimeApp(t)
			a.serverUI = server
			a.SetSidebarConfig(c, true)
			unsupported := strings.Contains(source, "version_check")
			if server && !unsupported {
				if a.configWarning != "" {
					t.Fatal("session warned", source, a.configWarning)
				}
			} else if !a.configWarningBanner || !strings.Contains(a.configWarning, "ikesu config check") {
				t.Fatal(source, a.configWarning)
			}
		}
	}
}
