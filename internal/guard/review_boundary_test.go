package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the literal reviewed escape forms plus their environment and
// file-operation variants. The CLI/shell tests consume the same table.
func TestReviewedShellBoundaries(t *testing.T) {
	root := t.TempDir()
	guard := filepath.Join(root, "guard")
	target := root + "/home/../victim"
	if e := os.MkdirAll(guard, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(guard, filepath.Join(root, "guard-link")); e != nil {
		t.Fatal(e)
	}
	input := Input{Cwd: root, LaunchCwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin", "X": "/bin/echo", "A": "/bin/echo", "IKESU_GUARD_MARKER": guard + "/agent-panes/p_1.json"}}
	data, e := os.ReadFile("testdata/review_cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct{ Line, Want string }
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	replace := strings.NewReplacer("${ROOT}", root, "${GUARD}", guard, "${TARGET}", target)

	for _, tc := range cases {
		t.Run(replace.Replace(tc.Line), func(t *testing.T) {
			_, v, e := lineVerdict(Config{}, input, replace.Replace(tc.Line))
			if v.Decision != tc.Want {
				t.Fatalf("got %+v (%v), want %s", v, e, tc.Want)
			}
		})
	}
}
