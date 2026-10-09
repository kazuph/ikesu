package runtime

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// Exercise the real emulator with the throughput workload's short CRLF rows.
// The comparison isolates recording overhead from PTY and rendering costs.
func BenchmarkRecordedThroughput(b *testing.B) {
	chunk := []byte(strings.Repeat("1999999 line of throughput test\r\n", 1024))
	for _, recorded := range []bool{false, true} {
		name := "emulator"
		if recorded {
			name = "recorded"
		}
		b.Run(name, func(b *testing.B) {
			p := &Pane{emu: vt.NewEmulator(160, 45)}
			p.emu.SetScrollbackSize(ScrollbackSize)
			b.SetBytes(int64(len(chunk)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if recorded {
					p.writeRecorded(chunk)
				} else {
					_, _ = p.emu.Write(chunk)
				}
			}
		})
	}
}
