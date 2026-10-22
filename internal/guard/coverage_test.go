package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginStartupAndSnapshotKeepShimFirst(t *testing.T) {
	d := t.TempDir()
	home := filepath.Join(d, "user")
	os.MkdirAll(home, 0700)
	for _, name := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
		code := "export PATH=/usr/bin:/bin\nexport HISTFILE=\"$ZDOTDIR/history\"\nprintf " + name + " >> \"$HOME/seen\"\n"
		if e := os.WriteFile(filepath.Join(home, name), []byte(code), 0600); e != nil {
			t.Fatal(e)
		}
	}
	a := NewAssets(d, "p_1", "/usr/bin/true", Config{})
	a.ConfigureShell("/bin/zsh")
	env, e := a.Prepare([]string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, d)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	if e = a.Sync("claude"); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("/bin/zsh", "-lic", `printf '%s|%s|%s\n' "$PATH" "$ZDOTDIR" "$HISTFILE"; export -p > "$HOME/snapshot"; /bin/zsh -c 'source "$HOME/snapshot"; printf "%s\n" "$PATH"; whence -p rm'`)
	cmd.Env = env
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal(string(b), e)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], a.Bin+":") || !strings.Contains(lines[0], "|"+home+"|"+home+"/history") || !strings.HasPrefix(lines[1], a.Bin+":") || lines[2] != filepath.Join(a.Bin, "rm") {
		t.Fatal(string(b))
	}
	seen, e := os.ReadFile(filepath.Join(home, "seen"))
	if e != nil || string(seen) != ".zshenv.zprofile.zshrc.zlogin.zshenv" {
		t.Fatal(string(seen), e)
	}
}

func TestSameAgentRepairsAssetsAndHandoff(t *testing.T) {
	d := t.TempDir()
	a := NewAssets(d, "p_1", "/usr/bin/true", Config{})
	if _, e := a.Prepare([]string{"HOME=" + d, "PATH=/bin", "IKESU_PANE_ID=p1"}, d); e != nil {
		t.Fatal(e)
	}
	if e := a.Sync("claude"); e != nil {
		t.Fatal(e)
	}
	os.Remove(a.MarkerPath)
	os.Remove(filepath.Join(a.Bin, "rm"))
	a = Import(a.Transfer())
	t.Cleanup(func() { a.Close() })
	if e := a.Sync("claude"); e != nil {
		t.Fatal(e)
	}
	if !regularFile(a.MarkerPath) || !regularFile(filepath.Join(a.Bin, "rm")) {
		t.Fatal("missing repaired assets")
	}
}

func TestUninjectedAndUnsupportedNeverPublishMarker(t *testing.T) {
	for _, shell := range []string{"disabled", "/bin/sh", "/bin/bash"} {
		t.Run(shell, func(t *testing.T) {
			d := t.TempDir()
			off := false
			c := Config{}
			if shell == "disabled" {
				c.Enabled = &off
			}
			a := NewAssets(d, "p_1", "/usr/bin/true", c)
			if shell != "disabled" {
				a.ConfigureShell(shell)
			}
			env, e := a.Prepare([]string{"HOME=" + d, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}, d)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { a.Close() })
			if EnvMap(env)["IKESU_GUARD_ENFORCING"] != "" {
				t.Fatal("unconfirmed shell suppresses ai_guard")
			}
			if e = a.Reconfigure(Config{}); e != nil {
				t.Fatal(e)
			}
			if e = a.Sync("claude"); e != nil {
				t.Fatal(e)
			}
			issues, e := CoverageIssues(d)
			if e != nil || len(issues) != 1 || issues[0].Pane != "p1" || issues[0].Reason == "" || issues[0].Recovery == "" || regularFile(a.MarkerPath) {
				t.Fatal(issues, e)
			}
			if e = a.Sync(""); e != nil {
				t.Fatal(e)
			}
			issues, e = CoverageIssues(d)
			if e != nil || len(issues) != 0 {
				t.Fatal("human pane left issue", issues, e)
			}
		})
	}
}
