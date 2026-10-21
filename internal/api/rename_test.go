package api

import "testing"

func TestStageOneNamesShareState(t *testing.T) {
	for _, exe := range []string{"/tmp/ikesu", "/tmp/gtx"} {
		ns := ExecutableNamespace(exe)
		if ns != "gtx" {
			t.Errorf("%s namespace=%s", exe, ns)
		}
		if got := stateRoot(ns, func(string) string { return "" }, "/home"); got != "/home/.local/state/gtx" {
			t.Errorf("root=%s", got)
		}
	}
}
func TestStageOneEnvironmentPriority(t *testing.T) {
	env := map[string]string{"GTX_STATE_DIR": "/old", "IKESU_STATE_DIR": "/new", "GTX_SOCKET_PATH": "/old/gtx.sock", "IKESU_SOCKET_PATH": "/new/gtx.sock"}
	get := func(k string) string { return env[k] }
	if got := stateRoot("gtx", get, "/home"); got != "/new" {
		t.Errorf("root=%s", got)
	}
	if got := APISocket("default", false, get); got != "/new/gtx.sock" {
		t.Errorf("socket=%s", got)
	}
	if got := APISocket("named", true, get); got != "/new/named/gtx.sock" {
		t.Errorf("explicit=%s", got)
	}
	if got := stateRoot("gtx-parity", get, "/home"); got != "/new/gtx-parity" {
		t.Errorf("isolated root=%s", got)
	}
}
