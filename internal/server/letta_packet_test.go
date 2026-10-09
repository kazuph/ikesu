package server

import (
	"testing"

	"github.com/kazuph/goterrace/internal/detect"
)

func TestLettaExplicitConversationRestoreAndDetection(t *testing.T) {
	testNativeRestore(t, "letta", "--conversation")
	for _, name := range []string{"letta", "letta-code"} {
		if got := detect.Identify(name, name); got != "letta" {
			t.Fatal(name, got)
		}
	}
	for _, tc := range []struct {
		input detect.Input
		state detect.State
	}{
		{detect.Input{Screen: "›"}, detect.Idle},
		{detect.Input{OSCTitle: "⠋ Thinking"}, detect.Working},
		{detect.Input{Screen: "Run this command?\nEnter to select · Esc to cancel"}, detect.Blocked},
	} {
		if got := detect.Detect("letta", tc.input); got.State != tc.state {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
}
