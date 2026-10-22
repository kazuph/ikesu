package guard

import "testing"

func TestStageOneProtectedCommandNames(t *testing.T) {
	for _, cmd := range []string{"ikesu", "herdr"} {
		input := Input{Command: cmd, Args: []string{"send", "*", "hello"}, Cwd: t.TempDir()}
		if got := Evaluate(Config{}, input); got.Decision != "block" {
			t.Fatalf("%s: %+v", cmd, got)
		}
	}
}
func TestStageOneGuardEnvironment(t *testing.T) {
	got := EnvMap([]string{"IKESU_PANE_ID=p1", "IKESU_PANE_ID=p2", "IKESU_GUARD_MARKER=/fresh"})
	if got["IKESU_PANE_ID"] != "p2" || got["IKESU_GUARD_MARKER"] != "/fresh" {
		t.Fatal(got)
	}
	got["IKESU_PANE_ID"] = "p3"
	out := environment(got)
	pairs := map[string]string{}
	for _, kv := range out {
		for n, c := range kv {
			if c == '=' {
				pairs[kv[:n]] = kv[n+1:]
				break
			}
		}
	}
	if pairs["IKESU_PANE_ID"] != "p3" {
		t.Fatal(pairs)
	}
}
func TestStageOneInlineIdentityPinned(t *testing.T) {
	i := Input{Env: EnvMap([]string{"IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=/original", "IKESU_SOCKET_PATH=/sock"})}
	commands, err := lineCommands(i, "IKESU_PANE_ID=p2 IKESU_GUARD_MARKER=/other IKESU_SOCKET_PATH=/other echo hello")
	if err != nil || len(commands) != 1 {
		t.Fatal(commands, err)
	}
	for key, want := range map[string]string{"IKESU_PANE_ID": "p1", "IKESU_GUARD_MARKER": "/original", "IKESU_SOCKET_PATH": "/sock"} {
		if commands[0].Env[key] != want {
			t.Fatalf("%s=%s", key, commands[0].Env[key])
		}
	}
}
