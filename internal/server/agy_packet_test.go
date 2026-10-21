package server

import (
	"testing"

	"github.com/kazuph/ikesu/internal/detect"
)

func TestAgyNativeConversationRestorePreservesIdentity(t *testing.T) {
	testNativeRestore(t, "agy", "--conversation")
	if got := detect.Identify("agy", "agy"); got != "agy" {
		t.Fatal(got)
	}
	if nativeResumeAgents["agy"].Source != "gtx:antigravity_cli" {
		t.Fatal("native source changed")
	}
	if got := resumeArgsFor("agy"); len(got) != 1 || got[0] != "--conversation" {
		t.Fatal(got)
	}
}
