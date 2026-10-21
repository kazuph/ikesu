package server

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginSharedStateRequiresMailboxJobSessionIsolation(t *testing.T) {
	teststate.Lock(t)
	first, done := startPluginServer(t)
	defer stopServer(t, first, done)
	other := api.SessionDir("other")
	otherDone := make(chan int, 1)
	go func() {
		code, err := Run("other", "0.1.0")
		if err != nil {
			t.Errorf("other server: %v", err)
		}
		otherDone <- code
	}()
	client := dialClient(t, other)
	client.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	defer client.conn.Close()
	defer stopServer(t, other, otherDone)
	root := t.TempDir()
	manifest := "id=\"isolation.test\"\nname=\"Isolation\"\nversion=\"0.1.0\"\nmin_gtx_version=\"0.1.0\"\n"
	if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, first, "plugin.link", map[string]any{"path": root}, nil)
	var plugins struct {
		Plugins []map[string]any `json:"plugins"`
	}
	apiOK(t, other, "plugin.list", map[string]any{}, &plugins)
	if len(plugins.Plugins) != 1 {
		t.Errorf("plugin registry was not shared: %+v", plugins)
	}
	apiOK(t, first, "pane.report_agent", map[string]any{"pane_id": "p1", "source": "custom:isolation", "agent": "codex", "state": "blocked"}, nil)
	apiOK(t, first, "msg.send", map[string]any{"from_agent": "isolation-sender", "to": "p1", "room": "isolation", "body": "FIRST_SESSION_ONLY"}, nil)
	var history api.MsgHistoryResult
	apiOK(t, other, "msg.history", map[string]any{"room": "isolation"}, &history)
	if len(history.Messages) != 0 {
		t.Errorf("other session sees first session mailbox: %+v", history.Messages)
	}
	var inbox api.MsgInboxResult
	apiOK(t, other, "msg.inbox", map[string]any{"room": "isolation", "to_agent": "p1"}, &inbox)
	if len(inbox.Messages) != 0 {
		t.Errorf("other session inbox leaked: %+v", inbox)
	}
	var rooms api.MsgRoomsResult
	apiOK(t, other, "msg.rooms", map[string]any{}, &rooms)
	if len(rooms.Rooms) != 0 {
		t.Errorf("other session rooms leaked: %+v", rooms)
	}
	var job api.RunStartedResult
	apiOK(t, first, "run.start", api.RunStartParams{CallerPane: firstPaneID(t, first), Label: "isolation-proof", Argv: []string{"/usr/bin/true"}, Completion: "none"}, &job)
	var out, err bytes.Buffer
	code := cli.Run([]string{"run", "list"}, cli.Options{Session: "other", Explicit: true, Stdout: &out, Stderr: &err})
	if code != 0 {
		t.Fatalf("other session run list: %d %s", code, &err)
	}
	if bytes.Contains(out.Bytes(), []byte(job.Job)) {
		t.Errorf("other session can list first session job %s: %s", job.Job, &out)
	}
	if response := apiReq(t, other, "run.cancel", api.RunCancelParams{JobID: job.Job}); response.Err == nil {
		t.Fatal("other session cancelled a job it does not own")
	}
	out.Reset()
	err.Reset()
	code = cli.Run([]string{"log", "--db"}, cli.Options{Session: "other", Explicit: true, Stdout: &out, Stderr: &err})
	if code != 0 || out.String() != filepath.Join(api.StateDir(), "sessions", "other", "gtx.db")+"\n" {
		t.Fatalf("other session database: %d %s %s", code, &out, &err)
	}
	out.Reset()
	err.Reset()
	code = cli.Run([]string{"run", "list"}, cli.Options{Session: "default", Stdout: &out, Stderr: &err, Getenv: func(key string) string {
		if key == "GTX_SOCKET_PATH" {
			return api.APISocketPath(first)
		}
		if key == "GTX_SOCKET_PATH_EXPLICIT" {
			return "1"
		}
		return os.Getenv(key)
	}})
	if code != 0 || !bytes.Contains(out.Bytes(), []byte(job.Job)) {
		t.Fatalf("inherited caller session lost its own job: %d %s %s", code, &out, &err)
	}
}
