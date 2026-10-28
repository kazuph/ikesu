package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// The panel test reads these actual persisted decisions through buildDialogSpec.
func TestTargetCommandDecisionFixtures(t *testing.T) {
	cases := []struct{ name, outer, reason string }{
		{"different", `printf ok; /bin/rm /outside/victim`, "対象プロジェクト: ikesu。必要な理由: 表示確認。実施内容: 実行せず却下。"},
		{"same", `/bin/rm /outside/victim`, "対象プロジェクト: ikesu。必要な理由: 表示確認。実施内容: 実行せず却下。"},
		{"free-prose", `printf ok; /bin/rm /outside/victim`, "検証用の申請です。外側と対象の表示を確かめます。"},
		{"reason-url", `printf ok; /bin/rm /outside/victim`, "検証用の申請です https://example.test/help"},
	}
	fixtures := map[string]api.Decision{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			a := NewAssets(root, "p_color", "/usr/bin/true", Config{})
			env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p_color", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err = a.Sync("claude"); err != nil {
				t.Fatal(err)
			}
			store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			input := Input{Cwd: root, Env: EnvMap(env)}
			input.Env["AI_GUARD_REQUEST_REASON"] = tc.reason
			var output bytes.Buffer
			allowed := AuthorizeLine(Config{}, root, input, tc.outer, &output, func(params api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
				decision, err := store.CreateDecision(params, time.Now())
				if err != nil {
					return decision, err
				}
				fixtures[tc.name] = decision
				return store.CancelDecision(decision.DecisionID, time.Now())
			})
			if allowed {
				t.Fatal("fixture command executed")
			}
			decision, ok := fixtures[tc.name]
			if !ok || decision.Origin == nil || decision.Body == nil {
				t.Fatalf("no actual decision: %s", output.String())
			}
			if decision.Origin.Command != tc.outer {
				t.Fatalf("outer command lost: %+v", decision.Origin)
			}
			if tc.name == "same" {
				if decision.Origin.TargetCommand != "" || strings.Contains(*decision.Body, "対象コマンド：") {
					t.Fatal("same command duplicated")
				}
			} else {
				if decision.Origin.TargetCommand != "/bin/rm /outside/victim" || !strings.Contains(*decision.Body, "対象コマンド："+decision.Origin.TargetCommand) {
					t.Fatalf("target lost: %+v body=%s", decision.Origin, *decision.Body)
				}
			}
		})
	}
	if dir := os.Getenv("IKESU_PANEL_DECISION_FIXTURE_DIR"); dir != "" {
		data, err := json.Marshal(fixtures)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "decisions.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
