package tui

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestNotificationTargetKeyKeepsTargetlessReloadToast(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.SetNavKey('o')
	a.ShowToast("reloaded config", "using config.toml", "", -1)
	toast := a.toast
	specFeed(a, "\x02o")
	if a.toast != toast || !a.toast.shown {
		t.Fatal("opening a missing notification target dismissed the reload toast")
	}
	pid := a.st.FocusedPane()
	a.ShowToast("finished", "shell", "finished", pid)
	specFeed(a, "\x02o")
	if a.toast != nil || a.st.FocusedPane() != pid {
		t.Fatal("valid notification target did not navigate and dismiss")
	}
	cfg, err := config.Decode([]byte("[keys]\nopen_notification_target='prefix+o'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(cfg, false)
	a.ShowToast("reloaded config", "using config.toml", "", -1)
	toast = a.toast
	specFeed(a, "\x02o")
	if a.toast != toast {
		t.Fatal("configured notification-target action dismissed a targetless toast")
	}
	a.ShowToast("finished", "shell", "finished", pid)
	specFeed(a, "\x02o")
	if a.toast != nil || a.st.FocusedPane() != pid {
		t.Fatal("configured notification-target action lost valid target navigation")
	}
}
