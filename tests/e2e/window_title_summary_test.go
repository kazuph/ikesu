package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// Match keepFailedE2EState: TestMain isolates GTX_* after package initialization.
var titleEvidenceDir = os.Getenv("GTX_TITLE_EVIDENCE")

func TestWindowTitleServerActualPTYTransitions(t *testing.T) {
	s := start(t)
	var raw strings.Builder
	s.observeOutput = func(b []byte) { raw.Write(b) }
	s.waitFor("shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var created api.WorkspaceCreatedResult
	// Keep the reporting pane in the background so completion remains unseen.
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: false}, &created)
	for _, step := range []struct{ state, mark string }{{"working", "🟡"}, {"blocked", "🔴"}, {"idle", "🔵"}} {
		before := raw.Len()
		s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: created.RootPane.PaneID, Source: "title-test", Agent: "codex", State: step.state}, nil)
		want := "\x1b]0;" + step.mark + " ikesu:" + s.name + "\x07"
		s.waitFor("outer title "+step.mark, 10*time.Second, func(_ string) bool { return strings.Contains(raw.String()[before:], want) })
		t.Logf("outer PTY title: %s ikesu:%s", step.mark, s.name)
	}
	before := raw.Len()
	if out, err := s.cli("workspace", "focus", created.Workspace.WorkspaceID); err != nil {
		t.Fatal(out, err)
	}
	want := "\x1b]0;ikesu:" + s.name + "\x07"
	s.waitFor("acknowledged outer title", 10*time.Second, func(_ string) bool { return strings.Contains(raw.String()[before:], want) })
	if out := titleEvidenceDir; out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "server.raw"), []byte(raw.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWindowTitleStandaloneBinaryActualPTYStartup(t *testing.T) {
	wrapper := filepath.Join(t.TempDir(), "named-standalone")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %q --session title-demo \"$@\"\n", binPath)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, true, wrapper, "")
	var raw strings.Builder
	s.observeOutput = func(b []byte) { raw.Write(b) }
	s.waitFor("standalone outer title", 10*time.Second, func(_ string) bool { return strings.Contains(raw.String(), "\x1b]0;ikesu:title-demo\x07") })
}
