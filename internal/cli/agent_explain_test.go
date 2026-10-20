package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/detect"
)

func TestAgentExplainFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "screen")
	if err := os.WriteFile(file, []byte("allow command?\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"agent", "explain", "--file", file, "--agent", "codex", "--json"}, Options{Stdout: &out, Stderr: &errOut})
	var e detect.Explanation
	if code != 0 || json.Unmarshal(out.Bytes(), &e) != nil || e.State != "blocked" {
		t.Fatalf("%d %s %s", code, &out, &errOut)
	}
	out.Reset()
	errOut.Reset()
	code = Run([]string{"agent", "explain", "--file", file, "--agent", "codex"}, Options{Stdout: &out, Stderr: &errOut})
	if code != 0 || !strings.Contains(out.String(), "rule: live_strong_blocker (region=after_last_prompt_marker priority=900)\nevidence: \"allow command?\\n\"") {
		t.Fatal(code, &out, &errOut)
	}
	for _, args := range [][]string{{}, {"--file", file}, {"p1", "--file", file, "--agent", "codex"}, {"--agent", "codex"}, {"p1", "--bogus"}} {
		if Run(append([]string{"agent", "explain"}, args...), Options{Stdout: &out, Stderr: &errOut}) != 2 {
			t.Fatal(args)
		}
	}
}
