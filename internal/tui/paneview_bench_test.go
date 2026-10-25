package tui

import (
	"io"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func BenchmarkPaneGridAlternatingViewport(b *testing.B) {
	cwd := b.TempDir()
	sup := runtime.NewSupervisor()
	a := NewApp(model.New(cwd, 160, 45), sup, io.Discard, cwd)
	p, err := sup.SpawnWith(a.focusedPane(), cwd, 140, 38, nil, []string{"/bin/cat"})
	if err != nil {
		b.Fatal(err)
	}
	defer sup.Kill(p.ID)
	p.WithEmu(func(e *vt.Emulator) { e.WriteString("日本e\u0301\r\nQUIET_PROMPT") })
	a.paneGrid(p, 0, 36, 81)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.paneGrid(p, 0, 36, 80+i%2)
	}
}
