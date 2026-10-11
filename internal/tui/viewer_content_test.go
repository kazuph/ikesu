package tui

import (
	"io"
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestViewerQuietPTYCropRetainsPromptAndPreservesScrollAndStandalone(t *testing.T) {
	teststate.Lock(t)
	for _, scenario := range []string{"quiet", "quiet-middle", "output-at-bottom"} {
		t.Run(scenario, func(t *testing.T) {
			flood := scenario == "output-at-bottom"
			a := specRuntimeApp(t)
			command := "printf 'QUIET_PROMPT\\n'; exec /bin/cat"
			marker := "QUIET_PROMPT"
			height := 30
			if scenario == "quiet-middle" {
				height = 60
				command = "printf '\\033[26;1HQUIET_PROMPT\\n'; exec /bin/cat"
			}
			if flood {
				command = "i=0; while [ $i -lt 100 ]; do printf 'ROW-%s\\n' $i; i=$((i+1)); done; printf 'BOTTOM_PROMPT\\n'; exec /bin/cat"
				marker = "BOTTOM_PROMPT"
			}
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 140, height, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, marker)
			v := a.NewViewer(io.Discard, 100, 24, nil)
			text := func(grid [][]uv.Cell) string {
				var out strings.Builder
				for _, row := range grid {
					for _, cell := range row {
						out.WriteString(cell.Content)
					}
				}
				return out.String()
			}
			local := a.paneGrid(p, 0, 20, 70)
			viewer := v.app.paneGrid(p, 0, 20, 70)
			if !strings.Contains(text(viewer), marker) {
				t.Fatal("viewer cropped out its actual PTY prompt", text(viewer))
			}
			if flood && !reflect.DeepEqual(viewer, local) {
				t.Fatal("live bottom output changed")
			}
			if !flood && strings.Contains(text(local), marker) {
				t.Fatal("standalone crop unexpectedly changed")
			}
			if flood && !reflect.DeepEqual(v.app.paneGrid(p, 3, 20, 70), a.paneGrid(p, 3, 20, 70)) {
				t.Fatal("scrollback crop changed")
			}
			if !flood && (!reflect.DeepEqual(v.app.paneGrid(p, 3, 20, 70), viewer) || !reflect.DeepEqual(a.paneGrid(p, 3, 20, 70), local)) {
				t.Fatal("absent scrollback did not clamp to each live viewport")
			}
		})
	}
}
