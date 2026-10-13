package childenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLostShimVariableStillRemovesAllStateBins(t *testing.T) {
	state := t.TempDir()
	first := filepath.Join(state, "guard", "bin", "p_1")
	second := filepath.Join(state, "gtx-other", "guard", "bin", "p_2")
	user := filepath.Join(state, "guard", "binaries")
	env := []string{"HOME=" + t.TempDir(), "GTX_STATE_DIR=" + state, "PATH=" + strings.Join([]string{first, second, user, "/usr/bin", "/bin"}, string(os.PathListSeparator))}
	clean := envMap(Scrub(env))
	if clean["PATH"] != strings.Join([]string{user, "/usr/bin", "/bin"}, string(os.PathListSeparator)) {
		t.Fatal(clean)
	}
	if !IsGuardShimDirectory("/Users/previous/.local/state/gtx-old/guard/bin/p_5", env) {
		t.Fatal("known old HOME/namespace layout lost")
	}
	for _, path := range []string{user, state + "-other/guard/bin/p_1", "/tmp/user/bin", "/Users/previous/.local/state/editor/guard/bin"} {
		if IsGuardShimDirectory(path, env) {
			t.Fatal("user PATH was removed", path)
		}
	}
}
