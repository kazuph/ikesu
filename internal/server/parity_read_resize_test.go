package server

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestParityReadLogicalLineSurvivesSplitAndCloseRealServer(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	pid := firstPaneID(t, dir)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: pid, Text: "clear; for i in 1 2 3 4 5 6 7 8; do echo line$i; done; printf 'A%0298dZ\\n' 0; for i in $(seq 25); do echo tail$i; done; exec /bin/cat",
		Keys: []string{"enter"},
	}, nil)
	waitRead(t, dir, pid, "tail25")
	baseline := map[string]string{}
	check := func(stage string) {
		t.Helper()
		limit := uint32(1000)
		for _, strip := range []bool{true, false} {
			for _, source := range []string{"recent", "recent-unwrapped"} {
				var out api.PaneReadResultEnvelope
				apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: pid, Source: source, Lines: &limit, StripANSI: strip}, &out)
				key := source
				if !strip {
					key += " raw"
				}
				text := strings.TrimRight(out.Read.Text, "\n")
				if stage == "before resize" {
					baseline[key] = text
				} else if text != baseline[key] {
					t.Errorf("%s %s: retained rows changed\nbefore: %q\nafter: %q", stage, key, baseline[key], text)
				}
			}
			var out api.PaneReadResultEnvelope
			apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: pid, Source: "recent-unwrapped", Lines: &limit, StripANSI: strip}, &out)
			found := false
			for _, line := range strings.Split(out.Read.Text, "\n") {
				if strings.HasPrefix(line, "A") {
					found = true
					if len(line) != 300 || !strings.HasSuffix(line, "Z") {
						t.Fatalf("%s strip=%v: long line length=%d, want 300 ending Z: %q", stage, strip, len(line), line)
					}
				}
			}
			if !found {
				t.Fatalf("%s: long line missing: %q", stage, out.Read.Text)
			}
		}
	}
	check("before resize")
	var split api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &pid, Direction: "down"}, &split)
	check("height shrunk")
	apiOK(t, dir, api.MethodPaneClose, api.PaneTarget{PaneID: split.Pane.PaneID}, nil)
	check("height restored")
}
