package e2e

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEndpointWindowTitleBelongsToEachAttachedTerminal(t *testing.T) {
	first := start(t)
	var firstRaw strings.Builder
	first.observeOutput = func(bytes []byte) { firstRaw.Write(bytes) }
	first.waitFor("first shell", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	second := startEnv(t, map[string]string{"IKESU_STATE_DIR": first.stateDir})
	var secondRaw strings.Builder
	second.observeOutput = func(bytes []byte) { secondRaw.Write(bytes) }
	second.waitFor("second shell", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first.send("printf '\\033]2;PANE_TITLE_A\\007'; printf 'FIRST_%s\\n' TITLE_OK\r")
	first.waitFor("first pane title", 5*time.Second, func(text string) bool { return strings.Contains(text, "FIRST_TITLE_OK") })
	second.send("\x02c\r")
	second.waitFor("new tab shell", 5*time.Second, func(text string) bool {
		return strings.Contains(text, "%2 terminal") && strings.Contains(text, "❯")
	})
	second.send("printf '\\033]2;PANE_TITLE_B\\007'; printf 'SECOND_%s\\n' TITLE_OK\r")
	second.waitFor("second pane title", 5*time.Second, func(text string) bool { return strings.Contains(text, "SECOND_TITLE_OK") })
	first.pump(time.Now().Add(100 * time.Millisecond))
	if !strings.Contains(first.text(), "FIRST_TITLE_OK") || strings.Contains(first.text(), "SECOND_TITLE_OK") {
		t.Fatal("second terminal changed the first view", first.text())
	}
	titles := regexp.MustCompile("\x1b\\][012];([^\x07\x1b]*)(?:\x07|\x1b\\\\)")
	for name, raw := range map[string]string{"first": firstRaw.String(), "second": secondRaw.String()} {
		found := titles.FindAllStringSubmatch(raw, -1)
		if len(found) == 0 {
			t.Fatalf("%s terminal did not receive its window title", name)
		}
		for _, title := range found {
			if title[1] != "ikesu:"+first.name {
				t.Fatalf("%s terminal received pane title as window title: %q", name, title[1])
			}
		}
	}
}
