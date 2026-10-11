package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

// B sends no input after its marker, so shell redraw cannot hide a loss.
func TestEndpointOtherViewerOperationsRetainQuietPaneContent(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	a, b := dialClient(t, dir), dialClient(t, dir)
	watch := func(c *testConn) <-chan render.Input {
		ch := make(chan render.Input, 256)
		go func() {
			defer close(ch)
			var decoder endpoint.SnapshotDecoder
			for {
				message, err := readClientMsg(c.rd)
				if err != nil {
					return
				}
				if message.Type != "snapshot" {
					continue
				}
				data, err := decode64(message.Data)
				if err != nil {
					t.Error(err)
					return
				}
				in, err := decoder.Decode(data)
				if err != nil {
					t.Error(err)
					return
				}
				ch <- in
			}
		}()
		return ch
	}
	ac, bc := watch(a), watch(b)
	defer func() {
		a.conn.Close()
		b.conn.Close()
		for range ac {
		}
		for range bc {
		}
	}()
	wait := func(ch <-chan render.Input, pred func(render.Input) bool) render.Input {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case in, ok := <-ch:
				if !ok {
					t.Fatal("viewer disconnected")
				}
				if pred(in) {
					return in
				}
			case <-timer.C:
				t.Fatal("expected snapshot did not arrive")
			}
		}
	}
	a.send(t, clientMsg{Type: "hello", Cols: 140, Rows: 30})
	wait(ac, func(in render.Input) bool { return len(in.Panes) > 0 })
	b.send(t, clientMsg{Type: "hello", Cols: 100, Rows: 24})
	wait(bc, func(in render.Input) bool { return len(in.Panes) > 0 })
	b.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'QUIET_%s\\n' VIEWER\n"))})
	content := func(in render.Input) string {
		out := render.Compose(in)
		var text strings.Builder
		for y := 0; y < out.Frame.H; y++ {
			for x := 0; x < out.Frame.W; x++ {
				if cell := out.Frame.Cell(x, y); cell != nil {
					text.WriteString(cell.Content)
				}
			}
		}
		return text.String()
	}
	baseline := wait(bc, func(in render.Input) bool { return strings.Contains(content(in), "QUIET_VIEWER") })
	pid := baseline.State.FocusedPane()
	assertB := func(label string, duration time.Duration) {
		t.Helper()
		timer := time.NewTimer(duration)
		defer timer.Stop()
		n := 0
		for {
			select {
			case in, ok := <-bc:
				if !ok {
					t.Fatal("B disconnected", label)
				}
				if in.State.ActiveWorkspace().TabRef().ID != baseline.State.ActiveWorkspace().TabRef().ID || !strings.Contains(content(in), "QUIET_VIEWER") {
					t.Fatalf("%s lost quiet B pane %d: %s", label, pid, content(in))
				}
				n++
			case <-timer.C:
				if n == 0 {
					t.Fatal("no B snapshots", label)
				}
				t.Logf("%s: %d B frames retain pane %d", label, n, pid)
				return
			}
		}
	}
	input := func(data string) { a.send(t, clientMsg{Type: "input", Data: encode64([]byte(data))}) }
	input("\x02c\r")
	wait(ac, func(in render.Input) bool {
		return len(in.State.ActiveWorkspace().Tabs) == 2 && in.State.FocusedPane() != pid
	})
	assertB("new tab, five seconds without B input", 5*time.Second)
	input("\x02p")
	wait(ac, func(in render.Input) bool { return in.State.FocusedPane() == pid })
	assertB("tab switch", time.Second)
	input("\x02v")
	wait(ac, func(in render.Input) bool { return len(in.State.ActiveWorkspace().TabRef().Layout.PaneIDs()) == 2 })
	assertB("split", time.Second)
	for _, size := range [][2]int{{180, 40}, {80, 20}, {140, 30}} {
		a.send(t, clientMsg{Type: "resize", Cols: size[0], Rows: size[1]})
		wait(ac, func(in render.Input) bool { return in.State.Cols == size[0] && in.State.Rows == size[1] })
		assertB(fmt.Sprint("shared tab resize ", size), time.Second)
	}
	input("\x02N")
	wait(ac, func(in render.Input) bool {
		return len(in.State.Workspaces) == 2 && in.State.ActiveWorkspace().ID != baseline.State.ActiveWorkspace().ID
	})
	assertB("new workspace", time.Second)
	a.conn.Close()
	assertB("other viewer disconnect", time.Second)
}
