package api

import "testing"

func TestExplicitStorageAndSession(t *testing.T) {
	env := map[string]string{"IKESU_STATE_DIR": "/new", "IKESU_SOCKET_PATH": "/custom/socket"}
	get := func(k string) string { return env[k] }
	if got := stateRoot("foo", get, "/home"); got != "/new" {
		t.Fatal(got)
	}
	if got := APISocket("default", false, get); got != "/custom/socket" {
		t.Fatal(got)
	}
	if got := APISocket("named", true, get); got != "/new/named/ikesu.sock" {
		t.Fatal(got)
	}
}
