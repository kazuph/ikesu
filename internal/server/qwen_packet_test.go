package server

import (
	"testing"

	"github.com/kazuph/goterrace/internal/detect"
)

func TestQwenNativeRestoreAndApprovalDetection(t *testing.T) {
	testNativeRestore(t, "qwen", "--resume")
	for _, name := range []string{"qwen", "qwen-code"} {
		if got := detect.Identify(name, name); got != "qwen" {
			t.Fatal(name, got)
		}
	}
	for _, input := range []detect.Input{
		{OSCTitle: "✳ Approval required"},
		{Screen: "⠋ Preparing...\nWaiting for user confirmation..."},
		{Screen: "apply this change?\nyes, allow once"},
	} {
		got := detect.Detect("qwen", input)
		if got.State != detect.Blocked || !got.VisibleBlocker {
			t.Fatalf("approval must be a visible blocker: %+v", got)
		}
	}
}
