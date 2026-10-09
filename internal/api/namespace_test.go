package api

import (
	"path/filepath"
	"testing"
)

func TestG9AlternateNamespaceAndInheritedSocket(t *testing.T) {
	for _, tc := range []struct{ exe, want string }{{"/tmp/gtx", "gtx"}, {"/tmp/gtx-next", "gtx-next"}, {"/tmp/gtx-next.exe", "gtx-next"}, {"/tmp/../危険", "gtx"}} {
		if got := ExecutableNamespace(tc.exe); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	getenv := func(k string) string {
		if k == "GTX_STATE_DIR" {
			return "/temporary/state"
		}
		return ""
	}
	base := stateRoot("gtx", getenv, "/home")
	alt := stateRoot("gtx-next", getenv, "/home")
	if base == alt || alt != filepath.Join(base, "gtx-next") {
		t.Fatal(base, alt)
	}
	if stateRoot("gtx-next", func(string) string { return "" }, "/home") != "/home/.local/state/gtx-next" {
		t.Fatal("default namespace")
	}
	for _, tc := range []struct {
		ns, socket     string
		explicit, want bool
	}{
		{"gtx-next", "/home/.local/state/gtx/default/gtx.sock", false, false},
		{"gtx-next", "/home/.local/state/gtx/default/gtx.sock", true, true},
		{"gtx-next", "/home/.local/state/gtx-next/default/gtx.sock", false, true},
		{"gtx", "/home/.local/state/gtx-next/default/gtx.sock", false, false},
		{"gtx", "/home/.local/state/gtx-next/default/gtx.sock", true, true},
		{"gtx", "/temporary/custom/default/gtx.sock", false, true},
	} {
		if inheritedSocket(tc.ns, tc.socket, tc.explicit) != tc.want {
			t.Fatal(tc)
		}
	}
}
