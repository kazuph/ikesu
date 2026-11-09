package guardmarker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 実物のdirenv、実bash、行ごとのguard呼び出しを使うため、1件が5秒を超える(負荷時は約8秒)。
type direnvFixture struct {
	t       *testing.T
	root    string
	env     []string
	dir     string
	target  string
	records func() string
}

func findHomebrewDirenv(t *testing.T) string {
	t.Helper()
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		link := filepath.Join(prefix, "bin", "direnv")
		if real, e := filepath.EvalSymlinks(link); e == nil && strings.HasPrefix(real, filepath.Join(prefix, "Cellar", "direnv")+"/") {
			return link
		}
	}
	t.Skip("標準の場所にHomebrewのdirenvがないため、実物のdirenvを使う試験をスキップします")
	return ""
}

func newDirenvFixture(t *testing.T, envrc string) *direnvFixture {
	t.Helper()
	a, env, target := newEntry(t, fixtureBinary)
	if e := a.Sync("codex"); e != nil {
		t.Fatal(e)
	}
	f := &direnvFixture{t: t, root: a.Root, target: target}
	f.dir = filepath.Join(a.Root, "proj")
	if e := os.MkdirAll(f.dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(f.dir, ".envrc"), []byte(strings.ReplaceAll(envrc, "TARGET", target)), 0600); e != nil {
		t.Fatal(e)
	}
	home := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			home = strings.TrimPrefix(kv, "HOME=")
		}
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=/opt/homebrew/bin:/usr/local/bin:/home/linuxbrew/.linuxbrew/bin:/usr/bin:/bin"
		}
	}
	f.env = append(env, "XDG_CONFIG_HOME="+home+"/cfg", "XDG_DATA_HOME="+home+"/data", "DIRENV_CONFIG="+home+"/cfg/direnv")
	f.records = func() string {
		b, _ := os.ReadFile(filepath.Join(a.Root, "guard", "decisions.jsonl"))
		return string(b)
	}
	return f
}

func (f *direnvFixture) run(name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Env = f.env
	c.Dir = f.dir
	b, e := c.CombinedOutput()
	return string(b), e
}

func (f *direnvFixture) allow(direnv string) {
	f.t.Helper()
	if out, e := f.run(direnv, "allow"); e != nil {
		f.t.Fatal(e, out)
	}
}

// 本物のdirenvが起動したbashの2行(eval ... && __main__ ... と "$@")は確認なしで通る。
func TestDirenvOwnBootstrapPassesWithoutAsk(t *testing.T) {
	t.Parallel()
	direnv := findHomebrewDirenv(t)
	f := newDirenvFixture(t, "export GUARD_DIRENV_FOO=bar\n")
	f.allow(direnv)
	out, e := f.run(direnv, "export", "json")
	if e != nil || !strings.Contains(out, `"GUARD_DIRENV_FOO": "bar"`) {
		t.Fatal(e, out)
	}
	if strings.Contains(f.records(), `"verdict":"ask"`) {
		t.Fatal("確認が出ました:", f.records())
	}
}

// .envrcの中の危ないコマンドは今までどおり確認になり、実行されない。
func TestDirenvEnvrcCommandsStillJudged(t *testing.T) {
	t.Parallel()
	direnv := findHomebrewDirenv(t)
	f := newDirenvFixture(t, "rm TARGET\n")
	f.allow(direnv)
	out, e := f.run(direnv, "export", "json")
	if e == nil {
		t.Fatal("direnvが成功しました", out)
	}
	if _, e := os.Stat(f.target); e != nil {
		t.Fatal("対象が消えました", e)
	}
	if !strings.Contains(f.records(), `"rule":"rm-paths"`) || !strings.Contains(f.records(), `"verdict":"ask"`) {
		t.Fatal(f.records())
	}
	if strings.Contains(f.records(), `"cmd_line":"\"$@\""`) {
		t.Fatal(`"$@" で確認が出ました:`, f.records())
	}
}

// 親がdirenvではないbashが同じ行を打っても確認が残る。
func TestDirenvLinesAskWhenParentIsNotDirenv(t *testing.T) {
	t.Parallel()
	direnv := findHomebrewDirenv(t)
	f := newDirenvFixture(t, "export A=1\n")
	for _, code := range []string{
		`eval "$("` + direnv + `" stdlib)" && __main__ source_env ` + filepath.Join(f.dir, ".envrc"),
		`__main__() { "$@"; }; __main__ true`,
	} {
		out, e := f.run("/bin/bash", "-c", code)
		if e == nil || !strings.Contains(out, "Command cancelled") {
			t.Fatalf("確認が出ませんでした: %s\n%v %s", code, e, out)
		}
	}
}

// direnvという名前の偽の実行ファイル(コピー、コピーを指すシンボリックリンク)が親の時は確認が残る。
func TestDirenvLookalikeParentAsks(t *testing.T) {
	t.Parallel()
	direnv := findHomebrewDirenv(t)
	real, err := filepath.EvalSymlinks(direnv)
	if err != nil {
		t.Fatal(err)
	}
	f := newDirenvFixture(t, "export A=1\n")
	fake := filepath.Join(f.root, "fakebin")
	if e := os.MkdirAll(fake, 0700); e != nil {
		t.Fatal(e)
	}
	body, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(fake, "direnv")
	if e := os.WriteFile(copyPath, body, 0700); e != nil {
		t.Fatal(e)
	}
	linkDir := filepath.Join(f.root, "linkbin")
	if e := os.MkdirAll(linkDir, 0700); e != nil {
		t.Fatal(e)
	}
	linkPath := filepath.Join(linkDir, "direnv")
	if e := os.Symlink(copyPath, linkPath); e != nil {
		t.Fatal(e)
	}
	for _, candidate := range []string{copyPath, linkPath} {
		f.allow(candidate)
		out, e := f.run(candidate, "export", "json")
		if e == nil || !strings.Contains(f.records(), `"verdict":"ask"`) {
			t.Fatalf("%s: 確認が出ませんでした: %v %s", candidate, e, out)
		}
	}
}
