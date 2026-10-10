package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestParityReadResizeWidthsAndAlternateRealPTY(t *testing.T) {
	for _, alt := range []bool{false, true} {
		name := "main"
		prefix := ""
		if alt {
			name, prefix = "alternate", "\\033[?1049h"
		}
		t.Run(name, func(t *testing.T) {
			s := NewSupervisor()
			defer s.KillAll()
			p, err := s.SpawnWith(1, t.TempDir(), 80, 12, nil, []string{"/bin/sh", "-c", "printf '" + prefix + "\\033[31mA%0298dZ\\033[0m\\nREADY' 0; exec /bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			waitReadMarker(t, p, "READY")
			for step, size := range [][2]int{{80, 5}, {80, 12}, {100, 12}, {80, 12}, {40, 12}, {80, 12}} {
				s.Resize(1, size[0], size[1])
				for _, source := range []string{"recent", "recent-unwrapped"} {
					plain, err := p.ReadLines(source, true)
					if err != nil {
						t.Fatal(err)
					}
					raw, err := p.ReadLines(source, false)
					if err != nil {
						t.Fatal(err)
					}
					if ansi.Strip(strings.Join(raw, "\n")) != strings.Join(plain, "\n") {
						t.Fatalf("%v %s: raw and plain content differ", size, source)
					}
					if !strings.Contains(strings.Join(raw, "\n"), "\x1b[31m") {
						t.Fatalf("%v %s: raw lost ANSI style", size, source)
					}
					// Compare the exact retained rows with the same test on main.
					data, _ := json.Marshal(strings.TrimRight(strings.Join(plain, "\n"), "\n"))
					t.Logf("READ %v %s %s", size, source, data)
					if source == "recent-unwrapped" && step < 2 && (len(plain[0]) != 300 || !strings.HasSuffix(plain[0], "Z")) {
						t.Fatalf("height resize changed the retained logical line: %q", plain)
					}
				}
			}
		})
	}
}
