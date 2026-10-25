package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
)

// Closed-history benchmarks alone miss continued history/main boundaries and
// raw output appended after the first indexed resize.
func BenchmarkRecordedBoundaryResizeScaling(b *testing.B) {
	for _, kind := range []string{"unclosed_plain", "unclosed_styled_wide", "indexed_then_output"} {
		for _, rows := range []int{ScrollbackSize / 10, ScrollbackSize} {
			b.Run(fmt.Sprintf("%s/rows=%d", kind, rows), func(b *testing.B) {
				b.ReportAllocs()
				var retained, charged int
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					p := &Pane{emu: vt.NewEmulator(160, 45)}
					p.emu.SetScrollbackSize(ScrollbackSize)
					p.emu.Scrollback().SetMaxBytes(config.DefaultScrollbackLimitBytes)
					switch kind {
					case "unclosed_plain":
						p.writeRecorded([]byte(strings.Repeat("x", 160*(rows+45))))
					case "unclosed_styled_wide":
						p.writeRecorded([]byte("\x1b[31m" + strings.Repeat("e\u0301 界", 160*(rows+45)/4)))
					case "indexed_then_output":
						p.writeRecorded([]byte(strings.Repeat("before\r\n", 45)))
						p.resizeTerminal(159, 45)
						p.writeRecorded([]byte(strings.Repeat("after e\u0301 界\r\n", rows)))
					}
					retained, charged = p.emu.ScrollbackLen(), p.emu.Scrollback().Bytes()
					b.StartTimer()
					p.resizeTerminal(158, 45)
					b.StopTimer()
					p.emu.Close()
				}
				b.ReportMetric(float64(retained), "history-rows")
				b.ReportMetric(float64(charged), "history-bytes")
			})
		}
	}
}
