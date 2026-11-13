package guard

import (
	"path/filepath"
	"testing"
	"time"
)

// An answer recorded by the server makes later asks in the same directory
// pass for the day, even if the asking command is gone.
func TestRecordDayApprovalSuppressesLaterAsks(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	// L2 の確認（開発向けのデプロイ）には、1日許可が効く。
	i := Input{Command: "npx", Args: []string{"wrangler", "deploy", "--env", "dev"}, Cwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin"}}
	v := Evaluate(Config{}, i)
	if v.Decision != "ask" || v.Tier != TierL2 {
		t.Fatalf("precondition: %+v", v)
	}
	if err := RecordDayApproval(state, root); err != nil {
		t.Fatal(err)
	}
	if v := applyGrants(state, v, time.Now()); v.Decision != "allow" || v.Suppressed != "day" || v.AllowedBy != "day" {
		t.Fatalf("day approval not applied: %+v", v)
	}
	// ほかの状態のフォルダの記録や、環境変数で指した別のファイルは読まない。
	if v := applyGrants(filepath.Join(root, "other"), Evaluate(Config{}, i), time.Now()); v.Decision != "ask" {
		t.Fatalf("grant leaked across state roots: %+v", v)
	}
}
