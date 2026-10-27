package tui

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func reviewWaitFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.TrimSpace(string(data)) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("PTY output %s: got %q want %q", path, data, want)
}

func reviewPTYSize(t *testing.T, a *App, pid int, w, h, cw, ch int) {
	t.Helper()
	if size := viewerTerminalSizes(a)[pid]; size != [2]int{w, h} {
		t.Fatalf("pane %d emulator size: got %v want %dx%d", pid, size, w, h)
	}
	file := filepath.Join(t.TempDir(), "size")
	a.sup.Input(pid, []byte(fmt.Sprintf("stty size > '%s'\n", file)))
	reviewWaitFile(t, file, fmt.Sprintf("%d %d", h, w))
	if gotW, gotH := a.sup.Get(pid).CellPixels(); gotW != cw || gotH != ch {
		t.Fatalf("pane %d pixels=%dx%d want %dx%d", pid, gotW, gotH, cw, ch)
	}
}

func TestViewerHeldInputDeliveryDoesNotReclaimSize(t *testing.T) {
	teststate.Lock(t)
	for _, expiry := range []bool{false, true} {
		for _, paste := range []bool{false, true} {
			for _, selectTab := range []bool{false, true} {
				t.Run(fmt.Sprintf("expiry=%t/paste=%t/select=%t", expiry, paste, selectTab), func(t *testing.T) {
					a := sizeOwnerRuntimeApp(t)
					if err := a.StartInitialPane(); err != nil {
						t.Fatal(err)
					}
					a.exec(model.CmdNewTab{Workspace: 0, NoFocus: true})
					// Readline may restore the PTY size captured while preparing
					// its first prompt. Start the ownership test after that.
					for id := range viewerTerminalSizes(a) {
						a.sup.Input(id, []byte("printf '\\n'; PS1='SIZE_READY> '\n"))
						specWaitText(t, a.sup.Get(id), "\nSIZE_READY>")
					}
					first := a.NewViewer(io.Discard, 196, 50, nil)
					first.Begin(a, 196*8, 50*16)
					second := a.NewViewer(io.Discard, 82, 30, nil)
					second.Begin(a, 82*10, 30*20)
					old := a.st.Workspaces[0].Tabs[0].Layout.Focused()
					target := old
					if selectTab {
						target = a.st.Workspaces[0].Tabs[1].Layout.Focused()
					}
					file := filepath.Join(t.TempDir(), "delivered")
					command := fmt.Sprintf("printf '%%s' $$ > '%s'\n", file)
					first.Input(a, []byte("\x1b[I"))
					if selectTab {
						first.Input(a, []byte{2, '2'})
					}
					if paste {
						command = "\x1b[200~" + command + "\x1b[201~"
					}
					first.Input(a, []byte(command))
					if len(first.heldKeys) == 0 {
						t.Fatal("input was not held")
					}
					second.Input(a, []byte("\x1b[<35;40;10M"))
					reviewPTYSize(t, a, old, 53, 26, 10, 20)
					if expiry {
						first.focusUntil = time.Now().Add(-time.Nanosecond)
						first.Tick(a)
					} else {
						first.Rendered(a, render.Output{}, first.FocusEpoch())
					}
					reviewWaitFile(t, file, strconv.Itoa(a.sup.Get(target).ShellPID()))
					reviewPTYSize(t, a, old, 53, 26, 10, 20)
					if selectTab {
						reviewPTYSize(t, a, target, 167, 46, 8, 16)
					}
				})
			}
		}
	}
}

func TestViewerSizeBasisLifetimeAfterPaneMove(t *testing.T) {
	teststate.Lock(t)
	for _, kind := range []string{"close", "existing-tab", "new-tab", "new-workspace", "remove-workspace", "leave-sibling"} {
		t.Run(kind, func(t *testing.T) {
			a := sizeOwnerRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			a.exec(model.CmdNewTab{Workspace: 0, NoFocus: true})
			if kind == "remove-workspace" {
				a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
				a.exec(model.CmdCloseTab{Workspace: 0, Tab: 1})
			}
			// Readline restores the size it read while preparing the first
			// prompt. Start this move contract after that initialization.
			for id := range viewerTerminalSizes(a) {
				a.sup.Input(id, []byte("printf '\\n'; PS1='SIZE_READY> '\n"))
				specWaitText(t, a.sup.Get(id), "\nSIZE_READY>")
			}
			v := a.NewViewer(io.Discard, 196, 50, nil)
			v.Begin(a, 196*8, 50*16)
			source := a.st.Workspaces[0].Tabs[0]
			moving := source.Layout.Focused()
			shell := a.sup.Get(moving).ShellPID()
			if kind == "leave-sibling" {
				v.with(a, func(ui *App) { ui.exec(model.CmdSplitFocused{Dir: layout.Vertical}) })
			}
			pids := map[int]int{}
			for id := range viewerTerminalSizes(a) {
				pids[id] = a.sup.Get(id).ShellPID()
			}
			if kind == "close" {
				a.exec(model.CmdCloseTab{Workspace: 0, Tab: 0})
				if a.sup.Get(moving) != nil {
					t.Fatal("closed pane still alive")
				}
			} else {
				dest := model.MoveDest{Kind: model.MoveDestTab, TabW: 0, TabT: 1, Split: layout.Vertical, Ratio: .5}
				switch kind {
				case "new-tab":
					dest = model.MoveDest{Kind: model.MoveDestNewTab, Workspace: 0}
				case "new-workspace":
					dest = model.MoveDest{Kind: model.MoveDestNewWorkspace, Cwd: t.TempDir()}
				case "remove-workspace":
					dest = model.MoveDest{Kind: model.MoveDestTab, TabW: 1, TabT: 0, Split: layout.Vertical, Ratio: .5}
				}
				out := model.PaneMoveOutcome{}
				a.exec(model.CmdMovePane{Pane: moving, Dest: dest, Out: &out})
				if !out.Changed || a.sup.Get(moving).ShellPID() != shell {
					t.Fatal("move changed process", out)
				}
				w := 167
				if kind == "existing-tab" || kind == "leave-sibling" || kind == "remove-workspace" {
					w = 82
				}
				reviewPTYSize(t, a, moving, w, 46, 8, 16)
				for _, id := range a.st.Workspaces[out.TgtWsIdx].Tabs[out.TgtTabIdx].Layout.PaneIDs() {
					reviewPTYSize(t, a, id, w, 46, 8, 16)
				}
				if kind == "leave-sibling" {
					for _, id := range source.Layout.PaneIDs() {
						reviewPTYSize(t, a, id, 167, 46, 8, 16)
					}
				}
				for id, pid := range pids {
					if a.sup.Get(id) == nil || a.sup.Get(id).ShellPID() != pid {
						t.Fatal("surviving process changed", id, pid)
					}
				}
			}
			live := map[*model.Tab]bool{}
			for _, ws := range a.st.Workspaces {
				for _, tab := range ws.Tabs {
					live[tab] = true
				}
			}
			for tab := range a.sizeOwners {
				if !live[tab] {
					t.Fatal("dead tab basis retained", tab.ID)
				}
			}
		})
	}
}

func TestViewerFrozenSizeAfterDisconnectAndHandoff(t *testing.T) {
	teststate.Lock(t)
	for _, handoff := range []bool{false, true} {
		t.Run(fmt.Sprint(handoff), func(t *testing.T) {
			a := sizeOwnerRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			v := a.NewViewer(io.Discard, 196, 50, nil)
			v.Begin(a, 196*8, 50*16)
			hidden := a.st.FocusedPane()
			shell := a.sup.Get(hidden).ShellPID()
			a.exec(model.CmdNewTab{Workspace: 0, NoFocus: true})
			v.Input(a, []byte{2, '2'})
			v.Detach(a)
			if handoff {
				var wire bytes.Buffer
				if err := gob.NewEncoder(&wire).Encode(a.HandoffState()); err != nil {
					t.Fatal(err)
				}
				var state HandoffState
				if err := gob.NewDecoder(&wire).Decode(&state); err != nil {
					t.Fatal(err)
				}
				a.RestoreHandoff(state)
			}
			a.st.Active = 0
			a.st.Workspaces[0].ActiveTab = 1
			small := a.NewViewer(io.Discard, 82, 30, nil)
			small.Begin(a, 82*10, 30*20)
			target := a.st.Workspaces[0].Tabs[1].Layout.Focused()
			a.exec(model.CmdSplitPane{Pane: target, Dir: layout.Vertical, Ratio: .5})
			reviewPTYSize(t, a, hidden, 167, 46, 8, 16)
			if a.sup.Get(hidden).ShellPID() != shell {
				t.Fatal("hidden process changed")
			}
			small.Input(a, []byte{2, '1'})
			reviewPTYSize(t, a, hidden, 53, 26, 10, 20)
		})
	}
}
