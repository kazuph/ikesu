package client

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/cellwidth"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineSnapshotCopy(t *testing.T) {
	for _, tc := range []struct {
		name, kind, text, notice         string
		raw, write, closed, unnegotiated bool
	}{
		{name: "inactive machine", kind: "copy_text", text: "日本語\nsecond\n", notice: "handoff snapshot をコピーしました（3 行）", write: true},
		{name: "exact limit", kind: "copy_text", text: strings.Repeat("a", 1<<20), notice: "handoff snapshot をコピーしました（1 行）", write: true},
		{name: "one byte over", kind: "copy_text", text: strings.Repeat("a", 1<<20+1), notice: "コピーできませんでした: "},
		{name: "malformed base64", kind: "copy_text", text: "!", notice: "コピーできませんでした: ", raw: true},
		{name: "terminal write fails", kind: "copy_text", text: "data", notice: "コピーできませんでした: ", closed: true},
		{name: "unnegotiated copy", kind: "copy_text", text: "ignored data", unnegotiated: true},
		{name: "ordinary OSC 52 remains hidden", kind: "output", text: "\x1b]52;c;aGlkZGVu\a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "terminal")
			out, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			if tc.closed {
				if err := out.Close(); err != nil {
					t.Fatal(err)
				}
			}
			methods := []string{endpoint.CopyTextMessage}
			if tc.unnegotiated {
				methods = nil
			}
			m := &machineClient{
				active: "local", out: out,
				endpoints: map[string]*managedEndpoint{"local": {}, "remote": {
					connection: &endpointConnection{writer: &writer{available: endpoint.Generation, methods: methods}},
				}},
			}
			data := tc.text
			if !tc.raw {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			m.event(endpointEvent{id: "remote", message: msg{Type: tc.kind, Data: data}})
			if strings.HasSuffix(tc.notice, ": ") {
				if !strings.HasPrefix(m.notice, tc.notice) || m.notice == tc.notice {
					t.Fatalf("failure notice = %q", m.notice)
				}
			} else if m.notice != tc.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tc.notice)
			}
			sink, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.write {
				want = fmt.Sprintf("\x1b]52;c;%s\a", data)
			}
			if string(sink) != want {
				t.Fatalf("terminal wrote %d bytes, want %d matching bytes", len(sink), len(want))
			}
		})
	}
}

func TestSnapshotHookFailureAppearsOnCurrentMachine(t *testing.T) {
	const cols, rows = 250, 25
	const reason = "pane\x1b[31m\x07 \x9b\u009b\u202e\x7f\tか\u3099 ready"
	const want = "コピーできませんでした: pane [31m か\u3099 ready"
	out, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	state := model.New(t.TempDir(), cols, rows)
	in := &render.Input{State: state}
	m := &machineClient{
		active: "local", order: []string{"local", "remote"}, out: out, size: tui.Winsize{Cols: cols, Rows: rows},
		endpoints: map[string]*managedEndpoint{
			"local": {snapshot: in},
			"remote": {connection: &endpointConnection{writer: &writer{
				available: endpoint.Generation, methods: []string{endpoint.CopyTextMessage},
			}}},
		},
	}
	m.event(endpointEvent{id: "remote", message: msg{
		Type: endpoint.CopyTextMessage, Reason: reason, Data: base64.StdEncoding.EncodeToString([]byte("ignored data")),
	}})
	sink, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if m.active != "local" || !strings.Contains(string(sink), want) || strings.Contains(string(sink), "\x1b]52;") {
		t.Fatalf("active=%q notice=%q terminal painted error=%t wrote OSC 52=%t",
			m.active, m.notice, strings.Contains(string(sink), want), strings.Contains(string(sink), "\x1b]52;"))
	}
	if !utf8.Valid(sink) {
		t.Fatal("terminal output contains invalid UTF-8")
	}
	title := render.WindowTitle(*in, m.title.session, m.title.groups)
	paintOutput := strings.Replace(string(sink), "\x1b]0;"+title+"\x07", "", 1)
	for _, control := range []string{"\x1b[31m", "\x07", "\u009b", "\u202e", "\x7f", "\t"} {
		if strings.Contains(paintOutput, control) {
			t.Fatalf("reason's control %q reached terminal output", control)
		}
	}
	emu := vt.NewEmulator(cols, rows)
	defer emu.Close()
	go io.Copy(io.Discard, emu)
	if _, err := emu.Write(sink); err != nil {
		t.Fatal(err)
	}
	bar := m.regions.ActionBar
	start := bar.Copy.X + bar.Copy.W
	var painted strings.Builder
	for x := start; x < start+cellwidth.String(want); x++ {
		if cell := emu.CellAt(x, bar.Area.Y); cell != nil {
			painted.WriteString(cell.Content)
		}
	}
	if painted.String() != want {
		t.Fatalf("rendered failure = %q, want %q", painted.String(), want)
	}
	mark := start + cellwidth.String(want[:strings.Index(want, "か")])
	if cell := emu.CellAt(mark, bar.Area.Y); cell == nil || cell.Content != "か\u3099" {
		t.Fatalf("combining mark did not stay in its base cell: %+v", cell)
	}
}
