package tui

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRestoreTypesOnlyExactRecordedSessionIntoRealPTY(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, "GTX_CONFIG", path)
	if err := os.WriteFile(path, []byte("[agent_restore.templates]\nclaude = \"printf 'RE%s:%s\\\\n' 'STORE_EXACT' '{session_id}'\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	a.SetSidebarConfig(cfg, false)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	pid := a.focusedPane()
	rp := a.sup.Get(pid)
	a.sup.Input(pid, []byte("printf 'RE%s\\n' 'ADY_LOCAL_RESTORE'\r"))
	specWaitText(t, rp, "READY_LOCAL_RESTORE")
	a.RecordSession(pid, AgentRecord{Agent: "claude", Kind: "id", Value: "sid-local-exact"})
	a.restoreLocalAgents(true)
	if a.toast == nil || a.toast.body != "would launch 1, skipped 0" {
		t.Fatal(a.toast)
	}
	a.restoreLocalAgents(false)
	specWaitText(t, rp, "RESTORE_EXACT:sid-local-exact")
	if a.toast == nil || a.toast.body != "launched 1, skipped 0" {
		t.Fatal(a.toast)
	}
	a.RecordSession(pid, AgentRecord{Agent: "claude", Kind: "id", Value: "--last"})
	a.restoreLocalAgents(false)
	if a.toast.body != "launched 0, skipped 1" {
		t.Fatal("unsafe id must fail closed", a.toast)
	}
	a.ClearSession(pid)
	a.restoreLocalAgents(false)
	if a.toast.body != "no pending restore" {
		t.Fatal("missing record must not infer a session", a.toast)
	}
}
