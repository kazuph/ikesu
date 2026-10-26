package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"strings"
	"testing"
)

var screenStorageBenchFrame []uv.Line

func BenchmarkScreenStorage(b *testing.B) {
	for _, op := range []string{"resize", "snapshot"} {
		b.Run(op, func(b *testing.B) {
			e := NewEmulator(160, 45)
			defer e.Close()
			row := strings.Repeat("x界e\u0301", 40)
			input := "\x1b[38;2;19;83;151;4;58;5;42m\x1b]8;;https://example.test/screen\x1b\\" + strings.Repeat(row+"\r\n", 44) + row + "\r"
			if _, err := e.Write([]byte(input)); err != nil {
				b.Fatal(err)
			}
			info := e.ScreenStorage()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if op == "resize" {
					e.scr.Resize(159+i%2, 45)
				} else {
					frame := make([]uv.Line, 45)
					for y := range frame {
						frame[y] = make(uv.Line, 160)
						e.CopyScreenRow(frame[y], 0, y)
					}
					if frame[0][0].Content == "" {
						b.Fatal("snapshot lost content")
					}
					screenStorageBenchFrame = frame
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(info.CellBytes*info.CellCapacity), "retained-cell-B")
			b.ReportMetric(float64(info.IDArrayBytes), "id-array-B")
			screenStorageBenchFrame = nil
		})
	}
}
