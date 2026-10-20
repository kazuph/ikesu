package envcompat

import "testing"

func TestLookupNames(t *testing.T) {
	for _, tc := range []struct{ newer, older, want string }{{"", "", ""}, {"", "old", "old"}, {"new", "", "new"}, {"same", "same", "same"}, {"new", "old", "new"}} {
		env := map[string]string{"IKESU_CONFIG": tc.newer, "GTX_CONFIG": tc.older}
		for _, key := range []string{"IKESU_CONFIG", "GTX_CONFIG"} {
			if got := Lookup(func(k string) string { return env[k] }, key); got != tc.want {
				t.Fatalf("%+v: %s=%q", tc, key, got)
			}
		}
	}
}
func TestBothNames(t *testing.T) {
	env := Both([]string{"GTX_PANE_ID=parent", "GTX_PANE_ID=child", "GTX_CONFIG=old", "IKESU_CONFIG=new", "HOME=/tmp/home"})
	values := map[string]string{}
	for _, kv := range env {
		for n, c := range kv {
			if c == '=' {
				values[kv[:n]] = kv[n+1:]
				break
			}
		}
	}
	for k, want := range map[string]string{"GTX_PANE_ID": "child", "IKESU_PANE_ID": "child", "GTX_CONFIG": "new", "IKESU_CONFIG": "new", "HOME": "/tmp/home"} {
		if values[k] != want {
			t.Fatalf("%s=%q want %q", k, values[k], want)
		}
	}
}
