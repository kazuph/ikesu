package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetsLifecycleAndShellChain(t *testing.T) {
	for _, shell := range []string{"/bin/zsh", "/bin/bash"} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			d := t.TempDir()
			home := filepath.Join(d, "home")
			orig := filepath.Join(d, "original")
			os.MkdirAll(home, 0700)
			os.MkdirAll(orig, 0700)
			startup := `export HISTFILE="$ZDOTDIR/history"; export GUARD_ORIG_SEEN=1; function git { printf user-function; }; alias gh='printf user-alias'` + "\n"
			if e := os.WriteFile(filepath.Join(orig, ".zshenv"), []byte(startup), 0600); e != nil {
				t.Fatal(e)
			}
			bashOrig := filepath.Join(d, "bash-orig")
			if e := os.WriteFile(bashOrig, []byte(startup), 0600); e != nil {
				t.Fatal(e)
			}
			a := NewAssets(d, "p_1", "/usr/bin/true", Config{})
			env, e := a.Prepare([]string{"HOME=" + home, "PATH=/usr/bin:/bin", "GTX_PANE_ID=p1", "ZDOTDIR=" + orig, "BASH_ENV=" + bashOrig}, d)
			if e != nil {
				t.Fatal(e)
			}
			entries, e := os.ReadDir(a.Bin)
			if e != nil || len(entries) != 0 {
				t.Fatal(entries, e)
			}
			code := `printf '%s|%s|%s|' "$ZDOTDIR" "$HISTFILE" "$GUARD_ORIG_SEEN"; git; printf '|'; alias gh`
			cmd := exec.Command(shell, "-c", code)
			cmd.Env = env
			b, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatal(string(b), e)
			}
			if !strings.Contains(string(b), "user-function") || !strings.Contains(string(b), "user-alias") || !strings.Contains(string(b), "|1|") {
				t.Fatal(string(b))
			}
			if !strings.Contains(string(b), orig+"|"+orig+"/history") {
				t.Fatal(string(b))
			}
			if e = a.Sync("codex"); e != nil {
				t.Fatal(e)
			}
			if !regularFile(a.MarkerPath) || !regularFile(filepath.Join(a.Bin, "git")) {
				t.Fatal("missing marker/shim")
			}
			if e = a.Sync(""); e != nil {
				t.Fatal(e)
			}
			entries, _ = os.ReadDir(a.Bin)
			if regularFile(a.MarkerPath) || len(entries) != 0 {
				t.Fatal("stale marker/shim")
			}
			a.Sync("codex")
			a.Close()
			a.Sync("codex")
			if regularFile(a.MarkerPath) {
				t.Fatal("marker resurrected after close")
			}
		})
	}
}
func TestDisabledAndIdentityless(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		d := t.TempDir()
		c := Config{}
		enabled := false
		if disabled {
			c.Enabled = &enabled
		}
		a := NewAssets(d, "p_1", "/usr/bin/true", c)
		env := []string{"PATH=/usr/bin:/bin"}
		if disabled {
			env = append(env, "GTX_PANE_ID=p1")
		}
		got, e := a.Prepare(env, d)
		if e != nil || strings.Join(got, "\n") != strings.Join(env, "\n") {
			t.Fatal(got, e)
		}
		if _, e = os.Stat(filepath.Join(d, "guard")); !os.IsNotExist(e) {
			t.Fatal("unexpected injection", e)
		}
	}
}

func TestModeEnvironmentAndEmptyTargets(t *testing.T) {
	for _, mode := range []string{"record", "enforce"} {
		d := t.TempDir()
		a := NewAssets(d, "p_1", "/usr/bin/true", Config{Mode: mode, Targets: []string{}})
		env, e := a.Prepare([]string{"HOME=" + d, "PATH=/usr/bin:/bin", "GTX_PANE_ID=p1"}, d)
		if e != nil {
			t.Fatal(e)
		}
		defer a.Close()
		if (EnvMap(env)["GTX_GUARD_ENFORCING"] == "1") != (mode == "enforce") {
			t.Fatal(env)
		}
		c := exec.Command("/bin/zsh", "-c", "printf SAFE_EMPTY_TARGETS")
		c.Env = env
		b, e := c.CombinedOutput()
		if e != nil || string(b) != "SAFE_EMPTY_TARGETS" {
			t.Fatal(string(b), e)
		}
	}
}

func TestRestoreInheritedGuardPreservesUserEnvironment(t *testing.T) {
	d := t.TempDir()
	a := NewAssets(d, "p_1", "/usr/bin/true", Config{})
	original := []string{"HOME=" + d, "PATH=/usr/bin:/bin", "ZDOTDIR=" + d + "/user", "BASH_ENV=" + d + "/user/bash-env", "GTX_PANE_ID=p1", "KEEP=unchanged"}
	prepared, e := a.Prepare(original, d)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	restored := EnvMap(RestoreInherited(prepared))
	for k, v := range EnvMap(original) {
		if k == "GTX_PANE_ID" {
			if restored[k] != "" {
				t.Fatal("parent pane identity survived cleanup")
			}
			continue
		}
		if restored[k] != v {
			t.Fatal(k, restored[k], v)
		}
	}
	for k := range restored {
		if strings.HasPrefix(k, "GTX_GUARD_") {
			t.Fatal("inherited guard env", k)
		}
	}
}
