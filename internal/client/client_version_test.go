package client

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestClientVersionNoticeClearsEntireLabel(t *testing.T) {
	const cols, rows = 160, 25
	in := render.Input{State: model.New(t.TempDir(), cols, rows)}
	file, err := os.CreateTemp(t.TempDir(), "display")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m := &machineClient{clientVersion: "old-client", active: "local", order: []string{"local"}, out: file, size: tui.Winsize{Cols: cols, Rows: rows}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &in}}}
	text := func() string {
		t.Helper()
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		emu := vt.NewEmulator(cols, rows)
		defer emu.Close()
		if _, err := emu.Write(data); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for x := 0; x < cols; x++ {
			if c := emu.CellAt(x, rows-1); c != nil {
				b.WriteString(c.Content)
			}
		}
		return b.String()
	}
	before := text()
	if !strings.Contains(before, "gtx vold-client") {
		t.Fatal(before)
	}
	m.notify("ok")
	during := text()
	if strings.Contains(during, "gtx") || strings.Contains(during, "old-client") || !strings.Contains(during, "ok") {
		t.Fatal(during)
	}
	// A new server snapshot cannot replace the display's own version.
	replacement := render.Input{State: model.New(t.TempDir(), cols, rows)}
	m.endpoints["local"].snapshot = &replacement
	m.advance(m.noticeUntil)
	after := text()
	if !strings.Contains(after, "gtx vold-client") || strings.Contains(after, "ok") {
		t.Fatal(after)
	}
}
