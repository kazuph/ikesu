package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestG2PaneMetadataSocketCLISequenceFilteringClearAndExpiry(t *testing.T) {
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	pid := firstPaneID(t, dir)
	apiOK(t, dir, "pane.report_agent", map[string]any{"pane_id": pid, "source": "status", "agent": "codex", "state": "working"}, nil)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"pane", "report-metadata", pid, "--source", "metadata", "--agent", "codex", "--applies-to-source", "status", "--title", " Task ", "--display-agent", "Coder", "--state-label", "working=Building", "--token", "custom=VALUE", "--seq", "2"}, cli.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(k string) string {
		if k == "GTX_SOCKET_PATH" {
			return api.APISocketPath(dir)
		}
		return ""
	}})
	if code != 0 || stdout.Len() != 0 {
		t.Fatal(code, stdout.String(), stderr.String())
	}
	get := func() api.PaneInfo {
		var out api.PaneInfoResult
		apiOK(t, dir, "pane.get", api.PaneTarget{PaneID: pid}, &out)
		return out.Pane
	}
	info := get()
	if info.Title == nil || *info.Title != "Task" || info.DisplayAgent == nil || *info.DisplayAgent != "Coder" || info.StateLabels["working"] != "Building" || info.Tokens["custom"] != "VALUE" {
		t.Fatal(info)
	}
	for _, seq := range []uint64{1, 2} {
		apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "metadata", "seq": seq, "tokens": map[string]string{"custom": "STALE"}}, nil)
		if get().Tokens["custom"] != "VALUE" {
			t.Fatal("stale sequence applied")
		}
	}
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "other", "agent": "claude", "applies_to_source": "missing", "title": "Wrong", "tokens": map[string]string{"custom": "SECOND"}}, nil)
	info = get()
	if *info.Title != "Task" || info.Tokens["custom"] != "SECOND" {
		t.Fatal("presentation filters affected tokens", info)
	}
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "metadata", "seq": 3, "clear_title": true, "clear_display_agent": true, "clear_state_labels": true, "tokens": map[string]any{"custom": nil}}, nil)
	info = get()
	if info.Title != nil || info.DisplayAgent != nil || len(info.StateLabels) != 0 || len(info.Tokens) != 0 {
		t.Fatal("clear failed", info)
	}
	apiOK(t, dir, "pane.report_agent", map[string]any{"pane_id": pid, "source": "missing", "agent": "claude", "state": "idle"}, nil)
	if info = get(); info.Title == nil || *info.Title != "Wrong" {
		t.Fatal("retained matching presentation not shown", info)
	}
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "expiry", "tokens": map[string]string{"expiry": "X"}, "ttl_ms": 1}, nil)
	time.Sleep(3 * time.Millisecond)
	if len(get().Tokens) != 0 {
		t.Fatal("expired tokens remained")
	}
	for _, extra := range []map[string]any{{"ttl_ms": 0}, {"tokens": map[string]string{"invalid.name": "X"}}} {
		p := map[string]any{"pane_id": pid, "source": "bad"}
		for k, v := range extra {
			p[k] = v
		}
		if apiReq(t, dir, "pane.report_metadata", p).Err == nil {
			t.Fatal("invalid metadata accepted", p)
		}
	}
	if apiReq(t, dir, "pane.report_metadata", map[string]any{"pane_id": "p9999", "source": "bad"}).Err == nil {
		t.Fatal("missing pane accepted")
	}
	many := map[string]string{}
	for i := 0; i < 17; i++ {
		many[string(rune('a'+i))] = "V"
	}
	params := map[string]any{"pane_id": pid, "source": "limits", "tokens": many}
	if apiReq(t, dir, "pane.report_metadata", params).Err == nil {
		t.Fatal("17 tokens accepted")
	}
	delete(many, "q")
	apiOK(t, dir, "pane.report_metadata", params, nil)
	other := map[string]string{}
	for i := 0; i < 16; i++ {
		other["other_"+string(rune('a'+i))] = "V"
	}
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "limits2", "tokens": other}, nil)
	if len(get().Tokens) != 32 {
		t.Fatal("32 token aggregate missing")
	}
	if apiReq(t, dir, "pane.report_metadata", map[string]any{"pane_id": pid, "source": "limits3", "tokens": map[string]string{"overflow": "V"}}).Err == nil {
		t.Fatal("33 tokens accepted")
	}

}

func TestG2PaneMetadataTitlePreservesRealGitBranch(t *testing.T) {
	repo := gitRepo(t)
	if out, err := exec.Command("git", "-C", repo, "branch", "-m", "metadata-branch").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	emu := vt.NewEmulator(120, 50)
	go io.Copy(io.Discard, emu)
	var screenMu sync.Mutex
	changed := make(chan struct{}, 1)
	readerDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(c.rd)
		for {
			var m clientMsg
			if err := decoder.Decode(&m); err != nil {
				readerDone <- err
				return
			}
			if m.Type != "output" {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(m.Data)
			if err != nil {
				readerDone <- err
				return
			}
			screenMu.Lock()
			emu.Write(raw)
			screenMu.Unlock()
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	}()
	defer func() {
		c.conn.Close()
		<-readerDone
		emu.Close()
	}()
	c.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 50})
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, "workspace.create", map[string]any{"cwd": repo, "focus": true}, &created)
	var panes api.PaneListResult
	apiOK(t, dir, "pane.list", api.PaneListParams{WorkspaceID: &created.Workspace.WorkspaceID}, &panes)
	if len(panes.Panes) != 1 {
		t.Fatal(panes)
	}
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": panes.Panes[0].PaneID, "source": "metadata", "title": "Task"}, nil)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	title := ""
	for {
		select {
		case <-changed:
			screenMu.Lock()
			title = ""
			for x := 26; x < 120; x++ {
				if cell := emu.CellAt(x, 1); cell != nil {
					title += cell.Content
				}
			}
			screenMu.Unlock()
			if strings.Contains(title, "Task metadata-branch") {
				return
			}
		case <-deadline.C:
			t.Fatal("metadata removed branch from rendered title", title)
		}
	}
}
