package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestSidebarContinuousStandaloneEveryCellRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Cols, a.st.Rows = 240, 60
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd, NoFocus: true})
	a.exec(model.CmdSplitFocused{})
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				a.SetReported(pid, "codex", "idle")
			}
		}
	}
	a.SetReported(2, "codex", "blocked")
	// The job has real output and an actual shell exit status. Its completed
	// record is supplied through the normal App job input, without a fake runner.
	log := filepath.Join(t.TempDir(), "job.log")
	cmd := exec.Command("/bin/sh", "-c", "printf 'CONTINUOUS_JOB_REAL\\n'")
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, data, 0600); err != nil {
		t.Fatal(err)
	}
	exit := int64(cmd.ProcessState.ExitCode())
	a.jobRows = []render.JobRow{{ID: "j1", Label: "CONTINUOUS_JOB", CallerPane: "p1", Status: "exited", ExitCode: &exit, LogPath: log, Cwd: a.cwd}}
	var output bytes.Buffer
	a.out = &output
	terminal := vt.NewEmulator(a.st.Cols, a.st.Rows)
	defer terminal.Close()
	type evidence struct {
		Side   string
		Jobs   bool
		Width  int
		Rows   []render.SidebarPosition
		Kernel []int
	}
	var records []evidence
	save := func(name string) {
		root := os.Getenv("SIDEBAR_CONTINUOUS_EVIDENCE")
		if root == "" {
			return
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		cells := make([][]string, a.st.Rows)
		for y := range cells {
			cells[y] = make([]string, a.st.Cols)
			for x := range cells[y] {
				cells[y][x] = terminal.CellAt(x, y).Content
			}
		}
		b, _ := json.Marshal(struct {
			Cols, Rows int
			Cells      [][]string
		}{a.st.Cols, a.st.Rows, cells})
		if err := os.WriteFile(filepath.Join(root, name+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	draw := func() {
		a.ResetEncoder()
		output.Reset()
		a.draw()
		if _, err := terminal.Write(output.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	drag := func(right bool, width int) {
		start, target := a.st.SidebarArea().W-1, width-1
		if right {
			start = a.st.RightSidebarArea().X
			target = a.st.Cols - width
		}
		sidebarEvent(a, input.KindMouseClick, start, 5)
		sidebarEvent(a, input.KindMouseMotion, target, 5)
		sidebarEvent(a, input.KindMouseRelease, target, 5)
	}
	for _, jobs := range []bool{false, true} {
		a.jobsPanel = jobs
		a.agentScroll = 0
		a.jobScroll = 0
		a.sidebarScroll = 0
		for _, right := range []bool{false, true} {
			drag(false, 26)
			drag(true, 26)
			expected := a.sidebarView().Rows
			_, upper, _ := a.st.SidebarBounds()
			side := "left"
			if right {
				side = "right"
			}
			for width := upper; width >= model.SidebarCollapsedWidth; width-- {
				drag(right, width)
				draw()
				rows := a.sidebarView().Rows
				if !reflect.DeepEqual(rows, expected) {
					t.Fatalf("%s jobs=%t width=%d changes row identities/order/Y", side, jobs, width)
				}
				in := render.Input{State: a.st, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, Jobs: a.jobRows, JobsPanel: jobs}
				// Compare the ANSI-decoded actual App frame with each target's status.
				for _, pos := range rows {
					if pos.Right != right {
						continue
					}
					r := pos.Row
					glyph := ""
					switch r.Kind {
					case render.RowWorkspace:
						glyph = "○"
						if r.Ws == 1 {
							glyph = "●"
						}
					case render.RowAgent:
						if r.Line == 0 {
							glyph = in.Panes[r.Pane].Status.Rune
						}
					case render.RowJob:
						glyph = "o"
					}
					x := min(1, width-2)
					if right {
						x = a.st.RightSidebarArea().X + min(2, width-1)
					}
					got := terminal.CellAt(x, pos.Y).Content
					if glyph != "" && got != glyph {
						t.Fatalf("%s jobs=%t width=%d row=%+v Y=%d got=%q want=%q", side, jobs, width, r, pos.Y, got, glyph)
					}
					if glyph == "" {
						expected := render.Compose(in).Frame.Cell(x, pos.Y).Content
						if got != expected {
							t.Fatalf("decoded non-state row differs: %s width=%d row=%+v got=%q want=%q", side, width, r, got, expected)
						}
					}
				}
				// Ask the kernel through the real pane shell, independently of render.
				ws := a.st.Workspaces[0]
				pid := ws.Tabs[0].Layout.PaneIDs()[0]
				pane := a.sup.Get(pid)
				rect := a.rectForPane(pid)
				marker := fmt.Sprintf("CONT_%s_%t_%d", side, jobs, width)
				command := fmt.Sprintf("set -- $(stty size); printf '\\n%s_%%s_%%s\\n' \"$1\" \"$2\"\r", marker)
				if _, err := pane.WriteToPTY([]byte(command)); err != nil {
					t.Fatal(err)
				}
				specWaitText(t, pane, fmt.Sprintf("%s_%d_%d", marker, rect.H, rect.W))
				records = append(records, evidence{side, jobs, width, rows, []int{rect.H, rect.W}})
				if width == 2 || width == 3 || width == 4 || width == 5 || width == 10 || width == 17 || width == 26 {
					save(fmt.Sprintf("standalone-%s-jobs%t-width%d", side, jobs, width))
				}
			}
			// A key toggle and a divider click must preserve the same row sequence.
			drag(right, 26)
			key := "\x02b"
			if right {
				key = "\x02B"
			}
			specFeed(a, key)
			if !reflect.DeepEqual(a.sidebarView().Rows, expected) {
				t.Fatal("key collapse changed rows")
			}
			draw()
			save(fmt.Sprintf("standalone-%s-key-jobs%t", side, jobs))
			drag(right, 26)
			x := a.st.SidebarArea().W - 1
			if right {
				x = a.st.RightSidebarArea().X
			}
			sidebarEvent(a, input.KindMouseClick, x, 5)
			sidebarEvent(a, input.KindMouseRelease, x, 5)
			a.flushSidebarDividerClicks(time.Now().Add(sidebarDividerDoubleClick + time.Nanosecond))
			if !reflect.DeepEqual(a.sidebarView().Rows, expected) {
				t.Fatal("divider collapse changed rows")
			}
			draw()
			save(fmt.Sprintf("standalone-%s-click-jobs%t", side, jobs))
		}
	}
	if root := os.Getenv("SIDEBAR_CONTINUOUS_EVIDENCE"); root != "" {
		b, _ := json.MarshalIndent(records, "", "  ")
		if err := os.WriteFile(filepath.Join(root, "standalone-rows.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PASS %d real-PTY widths: row identity/Y/order, right-edge status, kernel size, key and divider collapse", len(records))
}
