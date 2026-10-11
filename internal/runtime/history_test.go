package runtime

import (
	"github.com/charmbracelet/x/vt"
	"strings"
	"testing"
)

func TestHistoryReplayDoesNotExecuteTerminalRequests(t *testing.T) {
	p := recordedTestPane(t)
	p.ReplayHistory("\x1b]52;c;dXNlci1jbGlwYm9hcmQ=\a\x1b]0;stale-title\a\x1b[31mSAVED\x1b[m\r\n")
	rows, err := p.ReadLines("recent", false)
	if err != nil || !strings.Contains(strings.Join(rows, "\n"), "SAVED") || !strings.Contains(strings.Join(rows, "\n"), "\x1b[") || p.oscTitle != "" || len(p.clipboard) > 0 {
		t.Fatal(rows, err, p.oscTitle, p.clipboard)
	}
}
func BenchmarkByteBoundedRecordedThroughput(b *testing.B) {
	chunk := []byte(strings.Repeat("1999999 line of throughput test\r\n", 1024))
	for _, bounded := range []bool{false, true} {
		name := "line_ring"
		if bounded {
			name = "byte_ring"
		}
		b.Run(name, func(b *testing.B) {
			p := &Pane{emu: vt.NewEmulator(160, 45)}
			p.emu.SetScrollbackSize(ScrollbackSize)
			if bounded {
				p.emu.Scrollback().SetMaxBytes(10000000)
			}
			defer p.emu.Close()
			b.SetBytes(int64(len(chunk)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p.writeRecorded(chunk)
			}
		})
	}
}
