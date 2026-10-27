package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestScreenAnswerInterruptedBeforeEnterIsLoggedAndNotified(t *testing.T) {
	teststate.Lock(t)
	s, pane, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	logPath := filepath.Join(t.TempDir(), "server.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = log
	t.Cleanup(func() { os.Stderr = previous; log.Close() })
	text := "comment before interrupted Enter"
	done := make(chan api.Response, 1)
	go func() { done <- answerScreen(s, d, "", &text) }()
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "3"+text })
	pane.RecordExternalInput()
	response := <-done
	if response.Err == nil || response.Err.Code != "screen_prompt_input_failed" {
		t.Fatal(response)
	}
	keys, _ := os.ReadFile(input)
	if string(keys) != "3"+text {
		t.Fatalf("interrupted Enter was sent: %q", keys)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"screen prompt input failed", d.DecisionID, "p1", "pane_user_active"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("server.log missing %q: %s", want, data)
		}
	}
	s.app.Do(func() {
		if !s.app.NotificationBusy() {
			t.Error("delivery failure had no visible notification")
		}
	})
}
