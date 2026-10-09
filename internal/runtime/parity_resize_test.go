package runtime

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/teststate"
	"strings"
	"testing"
	"time"
)

func TestParityPTYResizeRetainsEveryPrintedRow(t *testing.T) {
	teststate.Lock(t)
	for _, cursor := range []int{1, 6, 12} {
		t.Run(fmt.Sprint(cursor), func(t *testing.T) {
			s := NewSupervisor()
			defer s.KillAll()
			command := fmt.Sprintf("i=1; while [ $i -le 30 ]; do printf 'ROW-%%02d\\n' $i; i=$((i+1)); done; printf 'READY\\033[%d;1H'; exec sleep 60", cursor)
			p, err := s.SpawnWith(1, t.TempDir(), 60, 12, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for !strings.Contains(p.ScreenText(), "READY") {
				if time.Now().After(deadline) {
					t.Fatal("PTY did not finish printing")
				}
				time.Sleep(10 * time.Millisecond)
			}
			s.Resize(1, 60, 5)
			var text strings.Builder
			for _, row := range p.BufferLines() {
				for _, cell := range row {
					text.WriteString(cell.Content)
				}
				text.WriteByte('\n')
			}
			for i := 1; i <= 30; i++ {
				line := fmt.Sprintf("ROW-%02d", i)
				if strings.Count(text.String(), line) != 1 {
					t.Fatalf("cursor %d lost or duplicated %s: %s", cursor, line, text.String())
				}
			}
			if !strings.Contains(p.ScreenText(), "ROW-30") || p.Snapshot().Cursor.Y >= 5 {
				t.Fatal("last content or cursor was lost", p.ScreenText(), p.Snapshot().Cursor)
			}
			s.Resize(1, 60, 12)
			if !strings.Contains(p.ScreenText(), "ROW-20") || !strings.Contains(p.ScreenText(), "ROW-30") || p.Snapshot().Cursor.Y >= 12 {
				t.Fatal("growing the PTY must restore saved rows and a valid cursor", p.ScreenText(), p.Snapshot().Cursor)
			}
		})
	}
}
