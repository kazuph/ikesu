package retained

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
)

func TestTrustedExecutableDoesNotCopy(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.CheckExecutable(exe); err != nil {
		t.Fatalf("test executable must be trusted: %v", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := Ensure("1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Fatalf("trusted executable unexpectedly created release directory: %v", err)
	}
}

func TestRetainReusesAndPreservesDifferentRelease(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	first := []byte("first executable")
	if err := os.WriteFile(source, first, 0755); err != nil {
		t.Fatal(err)
	}
	dst, err := Retain(source, "1.2.3", home)
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(home, ".local/lib/ikesu/releases/1.2.3/ikesu") {
		t.Fatal(dst)
	}
	info, err := os.Stat(dst)
	if err != nil || info.Mode().Perm() != 0555 {
		t.Fatalf("mode: %v %v", info, err)
	}
	again, err := Retain(source, "1.2.3", home)
	if err != nil || again != dst {
		t.Fatalf("reuse: %s %v", again, err)
	}
	second := []byte("different executable")
	if err := os.WriteFile(source, second, 0755); err != nil {
		t.Fatal(err)
	}
	hashed, err := Retain(source, "1.2.3", home)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(second)
	want := filepath.Join(home, ".local/lib/ikesu/releases", fmt.Sprintf("1.2.3+%x", sum)[:len("1.2.3+")+12], "ikesu")
	if hashed != want {
		t.Fatalf("hash path %s want %s", hashed, want)
	}
	old, err := os.ReadFile(dst)
	if err != nil || string(old) != string(first) {
		t.Fatalf("old release changed: %q %v", old, err)
	}
}

func TestRetainRejectsWritableDestination(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".local"), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(home, ".local"), 0775); err != nil {
		t.Fatal(err)
	}
	if _, err := Retain(source, "1.2.3", home); err == nil || !strings.Contains(err.Error(), filepath.Join(home, ".local")) || !strings.Contains(err.Error(), filepath.Join(home, ".local/lib/ikesu/releases/1.2.3/ikesu")) {
		t.Fatalf("writable destination error omitted path or reason: %v", err)
	}
}
