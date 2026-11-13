package guard

import (
	"bufio"
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

func readAskLines(t *testing.T, root string) []AskRecord {
	t.Helper()
	f, err := os.Open(AsksPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []AskRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r AskRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// 承認・却下・期限切れ・申請文なし・サーバーなしが asks.jsonl に1件ずつ残り、秘密は伏せられる。
func TestAuthorizeRecordsAsks(t *testing.T) {
	for _, tc := range []struct {
		name, reason, option, text, status, outcome string
		noAwait                                     bool
		allowed                                     bool
	}{
		{"approve", "対象の確認用デプロイを行うため", "approve", "", "answered", OutcomeApprove, false, true},
		{"day_allow", "対象の確認用デプロイを行うため", "day_allow", "", "answered", OutcomeDayAllow, false, true},
		{"reject", "対象の確認用デプロイを行うため", "reject", "token=fixture-secret は使わないで", "answered", OutcomeReject, false, false},
		{"expired", "対象の確認用デプロイを行うため", "", "", "expired", OutcomeExpired, false, false},
		{"cancelled", "対象の確認用デプロイを行うため", "", "", "cancelled", OutcomeCancelled, false, false},
		{"no_reason", "", "", "", "", OutcomeNoReason, false, false},
		{"no_server", "対象の確認用デプロイを行うため", "", "", "", OutcomeNoServer, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			a := NewAssets(root, "p1", "/usr/bin/true", Config{})
			env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Sync("codex"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			i := Input{Command: "npx", Args: []string{"wrangler", "deploy", "--env", "staging"}, Cwd: root, Env: EnvMap(env)}
			i.Env["AI_GUARD_REQUEST_REASON"] = tc.reason
			store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var await Await
			if !tc.noAwait {
				await = func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
					now := time.Now()
					d, err := store.CreateDecision(p, now)
					if err != nil {
						return d, err
					}
					switch tc.status {
					case "expired":
						return store.ExpireDecision(d.DecisionID, now.Add(timeout))
					case "cancelled":
						return store.CancelDecision(d.DecisionID, now)
					}
					ans := api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "test", OptionID: &tc.option}
					if tc.text != "" {
						ans.Text = &tc.text
					}
					return store.AnswerDecision(ans, now)
				}
			}
			var out bytes.Buffer
			if got := Authorize(Config{}, root, i, "shim", &out, await); got != tc.allowed {
				t.Fatalf("allowed=%v out=%q", got, out.String())
			}
			recs := readAskLines(t, root)
			if len(recs) != 1 {
				t.Fatalf("records=%d: %+v", len(recs), recs)
			}
			r := recs[0]
			if r.Outcome != tc.outcome || r.Rule != "danger-word" || r.Via != "shim" {
				t.Fatalf("record=%+v", r)
			}
			if r.PaneID != "p1" || r.Repo != root || r.Cwd != root || !strings.Contains(r.Command, "wrangler deploy") || r.TSEpoch == 0 || r.TS == "" {
				t.Fatalf("fields: %+v", r)
			}
			if strings.Contains(r.AnswerText, "fixture-secret") {
				t.Fatalf("secret leaked: %q", r.AnswerText)
			}
			if tc.option == "reject" && !strings.Contains(r.AnswerText, "使わないで") {
				t.Fatalf("answer_text=%q", r.AnswerText)
			}
			if tc.status == "answered" && r.DecisionID == "" {
				t.Fatal("decision_id missing")
			}
			if (tc.outcome == OutcomeNoReason || tc.outcome == OutcomeNoServer) && r.DecisionID != "" {
				t.Fatalf("decision_id should be empty: %+v", r)
			}
			st, err := os.Stat(AsksPath(root))
			if err != nil || st.Mode().Perm() != 0o600 {
				t.Fatalf("mode: %v %v", st, err)
			}
		})
	}
}

// 書き込めなくても判定の結果は変わらず、stderr に1行出るだけ。
func TestAskRecordFailureKeepsVerdict(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p1", "/usr/bin/true", Config{})
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(AsksPath(root), 0o700); err != nil { // ファイルの場所をフォルダにして書けなくする
		t.Fatal(err)
	}
	i := Input{Command: "npx", Args: []string{"wrangler", "deploy"}, Cwd: root, Env: EnvMap(env)}
	i.Env["AI_GUARD_REQUEST_REASON"] = "確認用のデプロイを行うため"
	var out bytes.Buffer
	if Authorize(Config{}, root, i, "shim", &out, nil) {
		t.Fatal("authorized without server")
	}
	if strings.Count(out.String(), "guard asks log:") != 1 {
		t.Fatalf("stderr=%q", out.String())
	}
}
