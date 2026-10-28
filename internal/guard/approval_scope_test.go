package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDayApprovalDirectoryTree(t *testing.T) {
	base := t.TempDir()
	approved := filepath.Join(base, "project")
	child := filepath.Join(approved, ".worktree", "feat", "voice-print")
	sibling := filepath.Join(base, "project-other")
	elsewhere := filepath.Join(base, "elsewhere")
	for _, dir := range []string{child, sibling, elsewhere} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	escape := filepath.Join(approved, "escape")
	if err := os.Symlink(elsewhere, escape); err != nil {
		t.Fatal(err)
	}
	prefixEscape := filepath.Join(approved, "prefix-escape")
	if err := os.Symlink(sibling, prefixEscape); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "approval")
	if err := WriteApproval(file, "day:"+physical(approved), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cwd string
		allow     bool
	}{
		{"same", approved, true}, {"descendant-worktree", child, true}, {"other-folder", elsewhere, false}, {"prefix-only", sibling, false}, {"symlink-outside", escape, false}, {"symlink-prefix-only", prefixEscape, false}, {"physical-descendant-alias", alias, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := Input{Command: "curl", Args: []string{"-X", "POST", "https://example.test"}, Cwd: tc.cwd, Env: map[string]string{"AI_GUARD_TEMP_APPROVAL_FILE": file, "PATH": "/usr/bin:/bin"}}
			verdict := Evaluate(Config{}, input)
			if (verdict.Decision == "allow") != tc.allow {
				t.Fatalf("verdict=%+v wantAllow=%v", verdict, tc.allow)
			}
			input.Command = "git"
			input.Args = []string{"reset", "--hard"}
			if verdict = Evaluate(Config{}, input); verdict.Decision != "block" {
				t.Fatal("day grant allowed prohibited command", verdict)
			}
		})
	}
	expired := filepath.Join(base, "expired")
	if err := WriteApproval(expired, "day:"+physical(approved), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if validApproval(expired, "day:"+physical(child), time.Now()) {
		t.Fatal("expired day grant accepted")
	}
	if validApproval(file, "day:"+filepath.Join(approved, "missing"), time.Now()) {
		t.Fatal("unresolvable path accepted")
	}
	if validApproval(file, "temporary:"+physical(child), time.Now()) {
		t.Fatal("day grant widened temporary key")
	}
}
