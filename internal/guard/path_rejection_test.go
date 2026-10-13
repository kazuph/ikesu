package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShimOnlyPathDoesNotIntroduceCurrentDirectoryFallback(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "guard", "bin", "p_1")
	if err := os.MkdirAll(shim, 0700); err != nil {
		t.Fatal(err)
	}
	// Existing executable data in cwd is not in the original search path.
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "probe"), nil, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	env := []string{"HOME=" + t.TempDir(), "GTX_STATE_DIR=" + root, "PATH=" + shim}
	if value, err := Resolve("probe", env); err == nil {
		t.Fatal("removed shims introduced a cwd fallback", value)
	}
	if value, err := Resolve(filepath.Join(shim, "probe"), env); err == nil {
		t.Fatal("explicit shim path was accepted", value)
	}
}
