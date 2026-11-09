package guard

import (
	"path/filepath"
	"testing"
)

// An answer recorded by the server makes later asks in the same directory
// pass for the day, even if the asking command is gone.
func TestRecordDayApprovalSuppressesLaterAsks(t *testing.T) {
	root := t.TempDir()
	approvals := filepath.Join(root, "approvals")
	t.Setenv("AI_GUARD_TEMP_APPROVAL_FILE", approvals)
	i := Input{Command: "npx", Args: []string{"wrangler", "deploy"}, Cwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin", "AI_GUARD_TEMP_APPROVAL_FILE": approvals}}
	if v := Evaluate(Config{}, i); v.Decision != "ask" {
		t.Fatalf("precondition: %+v", v)
	}
	if err := RecordDayApproval(root); err != nil {
		t.Fatal(err)
	}
	if v := Evaluate(Config{}, i); v.Decision != "allow" || v.Suppressed != "day" {
		t.Fatalf("day approval not applied: %+v", v)
	}
}
