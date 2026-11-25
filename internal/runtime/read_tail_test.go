package runtime

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestReadTailLinesMatchesLargeHistory(t *testing.T) {
	p := recordedTestPane(t)
	p.emu.Scrollback().SetMaxBytes(250 << 20)
	p.writeRecorded([]byte(strings.Repeat("row\r\n", 200000) + "\x1b[31m" + strings.Repeat("abcdefgh", 1200) + "END\x1b[0m"))
	if p.emu.ScrollbackLen() < 200000 {
		t.Fatalf("fixture retained only %d physical rows", p.emu.ScrollbackLen())
	}
	for _, tt := range []struct {
		source string
		strip  bool
		count  int
	}{
		{"recent-unwrapped", true, 8},
		{"recent_unwrapped", false, 2},
		{"recent", true, 8},
		{"visible", false, 20},
		{"visible", true, 0},
	} {
		all, err := p.ReadLines(tt.source, tt.strip)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.ReadTailLines(tt.source, tt.strip, tt.count)
		want := all[max(0, len(all)-tt.count):]
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("%s strip=%t count=%d: %d rows, want %d, error=%v", tt.source, tt.strip, tt.count, len(got), len(want), err)
		}
	}
	if _, err := p.ReadTailLines("invalid", true, 1); err == nil {
		t.Fatal("invalid source was accepted")
	}
}

func BenchmarkReadTailLinesHistorySize(b *testing.B) {
	for _, tt := range []struct {
		name  string
		lines int
	}{{"100", 100}, {"200000", 200000}} {
		b.Run(tt.name, func(b *testing.B) {
			p := &Pane{emu: vt.NewEmulator(8, 5)}
			defer p.emu.Close()
			p.emu.Scrollback().SetMaxBytes(250 << 20)
			p.writeRecorded([]byte(strings.Repeat("row\r\n", tt.lines)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := p.ReadTailLines("recent-unwrapped", true, 12); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
