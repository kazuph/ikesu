package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

func cliImportSource(t *testing.T) string {
	t.Helper()
	dir := teststate.TempDir(t)
	cwd, _ := json.Marshal(dir)
	text := `{"version":5,"active":0,"selected":0,"sidebar_width":27,"workspaces":[{"id":"s1","section":"none","identity_cwd":` + string(cwd) + `,"active_tab":0,"public_tab_numbers":[1],"next_public_tab_number":2,"tabs":[{"layout":{"Pane":1},"pane_order":[1],"focused":1,"root_pane":1,"panes":{"1":{"global_pane_number":1,"cwd":` + string(cwd) + `}}}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestImportHerdrDryRunAndCancellationNeverWrite(t *testing.T) {
	teststate.Lock(t)
	source := cliImportSource(t)
	root := teststate.TempDir(t)
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("onboarding=false\n"), 0600)
	teststate.Setenv(t, config.EnvVar, cfg)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	var out, errs bytes.Buffer
	o := Options{Session: "import-test", Explicit: true, Stdout: &out, Stderr: &errs}
	if code := Run([]string{"session", "import-herdr", "--source", source, "--dry-run"}, o); code != 0 {
		t.Fatalf("dry run: %d %s", code, &errs)
	}
	rd, wr, _ := os.Pipe()
	wr.WriteString("no\n")
	wr.Close()
	old := os.Stdin
	os.Stdin = rd
	defer func() { os.Stdin = old; rd.Close() }()
	if code := Run([]string{"session", "import-herdr", "--source", source}, o); code != 0 {
		t.Fatalf("cancel: %d %s", code, &errs)
	}
	if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("preview/cancel wrote state")
	}
}
func TestImportHerdrOfflineAppendAndErrorsPreserveTarget(t *testing.T) {
	teststate.Lock(t)
	source := cliImportSource(t)
	root := teststate.TempDir(t)
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("onboarding=false\n"), 0600)
	teststate.Setenv(t, config.EnvVar, cfg)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	var out, errs bytes.Buffer
	o := Options{Session: "offline", Explicit: true, Stdout: &out, Stderr: &errs}
	args := []string{"session", "import-herdr", "--source", source, "--yes"}
	if Run(args, o) != 0 {
		t.Fatal(&errs)
	}
	path := session.Path(api.SessionDir("offline"))
	f, err := session.Load(path)
	if err != nil || len(f.Workspaces) != 1 {
		t.Fatal(f, err)
	}
	if Run(args, o) != 0 {
		t.Fatal(&errs)
	}
	f, _ = session.Load(path)
	if len(f.Workspaces) != 2 || f.Workspaces[0].Tabs[0].Panes[0].ID == f.Workspaces[1].Tabs[0].Panes[0].ID {
		t.Fatal("append failed", f)
	}
	before, _ := os.ReadFile(path)
	lock, err := os.Open(api.LockPath(api.SessionDir("offline")))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if Run(args, o) == 0 {
		t.Fatal("unreachable live target accepted")
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	os.WriteFile(filepath.Join(source, "session.json"), []byte("{broken"), 0600)
	if Run(args, o) == 0 {
		t.Fatal("corrupt input accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failure modified target")
	}
}
