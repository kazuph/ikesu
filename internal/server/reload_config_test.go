package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestReloadConfigCLIAndAPIKeepPaneAndInvalidConfig(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"+text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("[ui]\nshow_tab_bar=false\n")
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	defer ready.conn.Close()
	before := firstPaneID(t, dir)
	var initial api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: before}, &initial)
	var out, stderr bytes.Buffer
	run := func() api.ConfigReloadResult {
		t.Helper()
		out.Reset()
		stderr.Reset()
		code := cli.Run([]string{"server", "reload-config"}, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &stderr})
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("code=%d stderr=%s", code, stderr.String())
		}
		var resp api.Response
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.Err != nil || resp.ID != "cli:server:reload-config" {
			t.Fatal(out.String(), err)
		}
		var result api.ConfigReloadResult
		if err := json.Unmarshal(resp.Result, &result); err != nil || result.Type != "config_reload" || result.Diagnostics == nil {
			t.Fatal(out.String(), err)
		}
		return result
	}
	write("[ui]\nshow_tab_bar=true\n")
	if got := run(); got.Status != "applied" || len(got.Diagnostics) != 0 {
		t.Fatal(got)
	}
	write("[ui\n")
	if got := run(); got.Status != "failed" || len(got.Diagnostics) != 1 || !strings.Contains(got.Diagnostics[0], "keeping current config") {
		t.Fatal(got)
	}
	write("[ui]\nshow_tab_bar=false\nsidebar_width='bad'\n[keys]\nnew_tab='prefix+zz'\n")
	if got := run(); got.Status != "partial" || len(got.Diagnostics) != 2 || !strings.Contains(strings.Join(got.Diagnostics, " "), "keeping current ui settings") || !strings.Contains(strings.Join(got.Diagnostics, " "), "disabling binding") {
		t.Fatal(got)
	}
	var after api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: before}, &after)
	if initial.Pane.TerminalID != after.Pane.TerminalID {
		t.Fatal("reload restarted the terminal", initial.Pane, after.Pane)
	}
	if resp := apiReq(t, dir, api.MethodServerReloadConfig, []string{"bad"}); resp.Err == nil || resp.Err.Code != api.ErrInvalidRequest {
		t.Fatal(resp)
	}
}
