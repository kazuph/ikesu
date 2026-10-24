package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
)

// Measure actual post-resize read paths, separately reporting the first call.
// The outer PTY display and end-to-end drag gates remain separate checks.
func BenchmarkPostResizeReadSurfaces(b *testing.B) {
	for _, surface := range []string{"read_recent", "copy_buffer", "detection", "scroll_frame", "alt_switch"} {
		b.Run(surface, func(b *testing.B) {
			p := &Pane{emu: vt.NewEmulator(160, 45)}
			defer p.emu.Close()
			p.emu.SetCallbacks(vt.Callbacks{AltScreen: func(on bool) { p.altScreen = on }})
			p.emu.Scrollback().SetMaxBytes(config.DefaultScrollbackLimitBytes)
			p.writeRecorded([]byte(strings.Repeat("\x1b[31mrow e\u0301 界\x1b[0m\r\n", ScrollbackSize)))
			p.resizeTerminal(159, 45)
			read := func() {
				switch surface {
				case "read_recent":
					if _, err := p.ReadLines("recent_unwrapped", false); err != nil {
						b.Fatal(err)
					}
				case "copy_buffer":
					p.BufferLines()
				case "detection":
					p.DetectionLines()
				case "scroll_frame":
					p.AgentAttachFrame(p.emu.ScrollbackLen()/2, 159, 45)
				case "alt_switch":
					p.writeRecorded([]byte("\x1b[?1049h"))
					p.DetectionScreenLines()
					p.writeRecorded([]byte("\x1b[?1049l"))
				}
			}
			first := time.Now()
			read()
			elapsed := time.Since(first)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				read()
			}
			b.ReportMetric(float64(elapsed.Nanoseconds()), "first-ns")
		})
	}
}
