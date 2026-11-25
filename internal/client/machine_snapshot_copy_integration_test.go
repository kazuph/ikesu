package client

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestRemoteHandoffSnapshotCopyAfterMachineSwitch(t *testing.T) {
	gates := t.TempDir()
	gate := newPruneToolGate(t, gates, "snapshot-git")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// Run real Git, holding only the snapshot builder's initial Git result.
	// The menu action and ensuing visibility change go over real sockets.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nreal=" + quote(git) + "\nroot=" + quote(gates) + "\n" + `
if [ "${1-}" = --no-optional-locks ] && [ "${7-}" = --is-inside-work-tree ] && [ -f "$root/snapshot-git.armed" ]; then
	"$real" "$@" || exit "$?"
	printf '%s\n' "$$" > "$root/snapshot-git.ready"
	read -r go < "$root/snapshot-git.resume"
	exit 0
fi
exec "$real" "$@"
`
	if err := os.WriteFile(filepath.Join(gates, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := newRightMachineHarnessConfigured(t, func(configPath string) string {
		cmd := exec.Command(git, "init", "-q", filepath.Dir(configPath))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("init Git fixture: %s %v", out, err)
		}
		teststate.Setenv(t, "PATH", gates+string(os.PathListSeparator)+os.Getenv("PATH"))
		return ""
	}, true)
	m := h.m
	h.paintNow()
	h.activate("remote")
	remote := m.endpoints["remote"]
	pid := remote.snapshot.State.FocusedPane()
	const marker = "SNAPSHOT_SWITCH_MARKER"
	h.api("remote", api.MethodPaneSendText, map[string]any{
		"pane_id": fmt.Sprintf("p%d", pid),
		"text":    "printf 'SNAPSHOT_SWITCH_%s\\n' MARKER\n",
	})
	h.until("remote PTY marker", func() bool { return strings.Contains(portraitPaneText(remote.snapshot, pid), marker) })

	paneRect := remote.snapshot.State.DisplayPaneRects()[0].Rect
	inner, _ := model.PaneContentRect(paneRect, remote.snapshot.State.PaneBorders)
	x, y := inner.X+2, inner.Y+2
	send := func(raw string) {
		t.Helper()
		var framer input.Framer
		for _, event := range framer.Push([]byte(raw)) {
			if err := m.input(event); err != nil {
				t.Fatal(err)
			}
		}
	}
	send(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x, y, x, y))
	h.until("remote pane menu", func() bool { return remote.snapshot.Menu != nil })
	menu := remote.snapshot.Menu
	var down int
	found := false
	for _, item := range menu.Items[menu.Sel+1:] {
		if !item.Separator && !item.Disabled {
			down++
		}
		if item.Label == "Copy handoff snapshot" && !item.Disabled {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("remote menu did not offer the snapshot copy")
	}
	gate.arm(t)
	send(strings.Repeat("\x1b[B", down) + "\r")
	gate.wait(t)
	h.until("remote menu closed during snapshot build", func() bool { return remote.snapshot.Menu == nil })
	h.activate("local")
	readTerminal := func() string {
		t.Helper()
		out, err := os.ReadFile(m.out.Name())
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	const prefix = "\x1b]52;c;"
	before := readTerminal()
	if strings.Contains(before, prefix) || strings.Contains(m.notice, "handoff snapshot") {
		t.Fatal("copy completed before the remote hook was released")
	}
	gate.release(t)
	h.until("client copy notice on Local", func() bool { return strings.HasPrefix(m.notice, "handoff snapshot をコピーしました") })
	h.paintNow()

	output := readTerminal()[len(before):]
	_, payload, ok := strings.Cut(output, prefix)
	if !ok {
		t.Fatal("Local terminal did not receive OSC 52")
	}
	payload, _, ok = strings.Cut(payload, "\a")
	text, err := base64.StdEncoding.DecodeString(payload)
	if !ok || err != nil || !strings.Contains(string(text), marker) || !strings.Contains(string(text), "## git status") {
		t.Fatalf("invalid handoff snapshot %q (decode error %v)", text, err)
	}
	wantNotice := fmt.Sprintf("handoff snapshot をコピーしました（%d 行）", strings.Count(string(text), "\n")+1)
	if m.active != "local" || m.notice != wantNotice || !strings.Contains(output, "handoff snapshot") {
		t.Fatalf("active=%q copy notice=%q want=%q", m.active, m.notice, wantNotice)
	}
}
