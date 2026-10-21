package childenv

import (
	"os"
	"testing"
)

func TestStageOneScrubIdentityPreserveSettings(t *testing.T) {
	env := []string{"PATH=/bin", "IKESU_PANE_ID=p1", "GTX_PANE_ID=p1", "GOTERRACE_PANE_ID=1", "IKESU_TAB_ID=s1:t1", "GTX_TAB_ID=s1:t1", "IKESU_WORKSPACE_ID=s1", "GTX_WORKSPACE_ID=s1", "IKESU_GUARD_MARKER=/old", "GTX_GUARD_MARKER=/old", "IKESU_STATE_DIR=/state", "GTX_STATE_DIR=/state", "IKESU_CONFIG=/config", "GTX_CONFIG=/config", "IKESU_MACHINE_REMOTE_BINARY=gtx", "GTX_MACHINE_REMOTE_BINARY=gtx"}
	got := envMap(Scrub(env))
	for _, key := range []string{"IKESU_PANE_ID", "GTX_PANE_ID", "GOTERRACE_PANE_ID", "IKESU_TAB_ID", "GTX_TAB_ID", "IKESU_WORKSPACE_ID", "GTX_WORKSPACE_ID", "IKESU_GUARD_MARKER", "GTX_GUARD_MARKER"} {
		if _, ok := got[key]; ok {
			t.Errorf("parent leaked: %s", key)
		}
	}
	for key, want := range map[string]string{"IKESU_STATE_DIR": "/state", "GTX_STATE_DIR": "/state", "IKESU_CONFIG": "/config", "GTX_CONFIG": "/config", "IKESU_MACHINE_REMOTE_BINARY": "gtx", "GTX_MACHINE_REMOTE_BINARY": "gtx"} {
		if got[key] != want {
			t.Errorf("setting lost %s=%s", key, got[key])
		}
	}
}
func TestStageOneScrubNewShellMarkers(t *testing.T) {
	got := envMap(Scrub([]string{"BASH_ENV=/tmp/guard/shell/p_1/bash-env", "IKESU_GUARD_BASH_ENV_ORIG=/user/bash", "ZDOTDIR=/tmp/guard/shell/p_1", "IKESU_GUARD_ZDOTDIR_ORIG=/user/zsh", "IKESU_GUARD_ZDOTDIR_SET=1"}))
	if got["BASH_ENV"] != "/user/bash" || got["ZDOTDIR"] != "/user/zsh" {
		t.Fatal(got)
	}
}

func TestStageOneProcessScrubKeepsSettingNamesAndValues(t *testing.T) {
	t.Setenv("GTX_STATE_DIR", "/old-state")
	t.Setenv("IKESU_STATE_DIR", "")
	t.Setenv("GTX_CONFIG", "/old-config")
	t.Setenv("IKESU_CONFIG", "/new-config")
	ScrubProcess()
	for key, want := range map[string]string{"GTX_STATE_DIR": "/old-state", "IKESU_STATE_DIR": "", "GTX_CONFIG": "/old-config", "IKESU_CONFIG": "/new-config"} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
}
