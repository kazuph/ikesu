package tui

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRestoreRejectsDuplicateSavedCodexIDs(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	a := specRuntimeApp(t)
	a.st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	saved := a.st.Snapshot()
	saved.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		p.Agent, p.SessionKind, p.SessionID = "codex", "id", "duplicate-session"
	})
	path := filepath.Join(t.TempDir(), "session.json")
	if err := session.Save(path, saved); err != nil {
		t.Fatal(err)
	}
	loaded, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a.RestoreSession(loaded)
	for _, dry := range []bool{true, false} {
		a.restoreLocalAgents(dry)
		want := "launched 0, skipped 2"
		if dry {
			want = "would launch 0, skipped 2"
		}
		if a.toast == nil || a.toast.body != want {
			t.Fatalf("dry=%v: %+v", dry, a.toast)
		}
	}
}

func TestLocalRestoreTypesOnlyExactRecordedSessionIntoRealPTY(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, "IKESU_CONFIG", path)
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
