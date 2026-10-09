package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestSPECConfiguredFocusHistoryAcrossTabsWorkspacesAndDeletedPane(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[keys]\nfocus_history_back=['prefix+u']\nfocus_history_forward=['prefix+i']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfgPath)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.SetPaneKeybinds(c)
	p1 := a.focusedPane()
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	p2 := a.focusedPane()
	a.exec(model.CmdNewTab{Workspace: 0})
	p3 := a.focusedPane()
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd})
	p4 := a.focusedPane()
	for _, want := range []int{p3, p2, p1} {
		a.prefixKey(uv.Key{Code: 'u'})
		if a.focusedPane() != want {
			t.Fatalf("back=%d want=%d history=%v", a.focusedPane(), want, a.focusHistory)
		}
	}
	for _, want := range []int{p2, p3, p4} {
		a.prefixKey(uv.Key{Code: 'i'})
		if a.focusedPane() != want {
			t.Fatalf("forward=%d want=%d", a.focusedPane(), want)
		}
	}
	// Prefix history works while the terminal is in its Vim-style copy view.
	for _, fullscreen := range []bool{false, true} {
		a.enterCopy(fullscreen)
		a.handleKey(uv.Key{Code: 'b', Mod: uv.ModCtrl}, nil)
		a.handleKey(uv.Key{Code: 'u'}, nil)
		if a.focusedPane() != p3 || a.cm != nil {
			t.Fatal("history back inside copy mode")
		}
		a.enterCopy(fullscreen)
		a.handleKey(uv.Key{Code: 'b', Mod: uv.ModCtrl}, nil)
		a.handleKey(uv.Key{Code: 'i'}, nil)
		if a.focusedPane() != p4 || a.cm != nil {
			t.Fatal("history forward inside copy mode")
		}
	}
	// Remove an inactive historical pane: traversal must continue past it.
	a.exec(model.CmdClosePane{Pane: p2})
	a.focusHistoryStep(false)
	if a.focusedPane() != p3 {
		t.Fatal("workspace back")
	}
	a.focusHistoryStep(false)
	if a.focusedPane() != p1 {
		t.Fatal("deleted pane not skipped", a.focusedPane())
	}
}

func TestSPECDefaultRenameAndHelpOrder(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.prefixKey(uv.Key{Code: 'p', Mod: uv.ModShift})
	if a.dialog == nil || a.dialog.d.Title != "Rename pane" {
		t.Fatal("rename binding")
	}
	a.dialogKey(uv.Key{Code: 'a', Text: "renamed"})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.st.PaneLabels[model.PaneID(a.focusedPane())] != "renamed" {
		t.Fatal("rename apply")
	}
	rows := a.helpRows()
	for i, r := range rows {
		if r.Cmd == "rename pane" && (r.Bind != "prefix+shift+p" || rows[i+1].Cmd != "edit scrollback" || rows[i+2].Cmd != "copy mode") {
			t.Fatal("observed rename/edit/copy order")
		}
		if strings.HasPrefix(r.Cmd, "focus history") && r.Bind != "unset" {
			t.Fatal("history default", r)
		}
	}
	n := len(rows)
	if rows[n-3].Cmd != "cycle pane previous" || rows[n-2].Cmd != "focus history back" || rows[n-1].Cmd != "focus history forward" {
		t.Fatal("history order")
	}
}

func TestSPECEditScrollbackInvokesEditorWithRetainedText(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf 'SCROLLBACK_MARKER\\r\\n'; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "SCROLLBACK_MARKER")
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor")
	output := filepath.Join(dir, "retained.txt")
	if err = os.WriteFile(editor, []byte("#!/bin/sh\ncp \"$1\" \"$GTX_TEST_EDITOR_OUTPUT\"\nrm \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "EDITOR", editor)
	teststate.Setenv(t, "GTX_TEST_EDITOR_OUTPUT", output)
	a.prefixKey(uv.Key{Code: 'e'})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, err := os.ReadFile(output); err == nil && strings.Contains(string(b), "SCROLLBACK_MARKER") {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("editor did not receive retained text")
		case <-tick.C:
		}
	}
	if a.toast == nil || a.toast.title != "opened scrollback" || a.focusedPane() != pid {
		t.Fatal("editor result/focus")
	}
}
