package guard

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestReasonCancellationHint(t *testing.T) {
	for _, key := range []string{"AI_GUARD_REQUEST_REASON", "PROJECT_APPROVAL_REASON"} {
		for _, reason := range []string{"", " \t\n\u3000", "Please approve this command"} {
			t.Run(key+"/"+reason, func(t *testing.T) {
				root := t.TempDir()
				a := NewAssets(root, "p1", "/usr/bin/true", Config{})
				env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
				if err != nil {
					t.Fatal(err)
				}
				if err := a.Sync("codex"); err != nil {
					t.Fatal(err)
				}
				i := Input{Command: "dd", Cwd: root, Env: EnvMap(env)}
				i.Env[key] = reason
				if key != "AI_GUARD_REQUEST_REASON" {
					i.Env["AI_GUARD_REQUEST_ENV_NAME"] = key
					i.Env["AI_GUARD_REQUEST_REASON"] = "別の変数は読まない"
				}
				var out bytes.Buffer
				if Authorize(Config{}, root, i, "shim", &out, nil) {
					t.Fatal("invalid reason authorized")
				}
				want := "❌ Command cancelled: dd\n   この guard メッセージはユーザー本人の指示です。\n"
				if reason == "Please approve this command" {
					want += "   理由: 要承認コマンドの申請文は日本語で書いてください。英語の申請文は禁止です。\n   例: " + key + "='なぜ必要か、何を確認済みか、戻し方は何かを日本語で書く' <command>\n"
				} else {
					want += "   理由: 要承認コマンドには、対象プロジェクト・必要な理由・実施内容を説明する申請文が必要です。\n   例: " + key + "='対象プロジェクト: <名称>。必要な理由: <解決したい課題>。実施内容: <行う変更や操作>。確認済み: <確認内容>。戻し方: <戻す手順>。' <command>\n   英語の申請文は禁止です。\n"
				}
				want += "   判定理由: 実行前に承認が必要です。\n   承認が必要な理由: 実行前に承認が必要です。（規則: dangerous）\n   引っかかったコマンド: dd\n"
				if out.String() != want {
					t.Fatalf("got:\n%swant:\n%s", &out, want)
				}
				b, err := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				var record Record
				if err := json.Unmarshal(b, &record); err != nil {
					t.Fatal(err)
				}
				if record.Decision != "ask" || record.Rule != "dangerous" || record.Via != "shim" || record.CommandLine != "dd" {
					t.Fatalf("guard verdict/log changed: %+v", record)
				}
			})
		}
	}
}

// Exercise actual persisted decisions through the guard's Await dependency.
func TestDecisionAnswerFeedback(t *testing.T) {
	for _, tc := range []struct {
		name, option, text, status, want string
		allowed                          bool
	}{
		{"approve", "approve", "", "answered", "", true},
		{"approve_comment", "approve", "ええやん", "answered", "", true},
		{"day_allow", "day_allow", "", "answered", "", true},
		{"reject_comment", "reject", "実行しないで", "answered", "理由: 実行しないで", false},
		{"reject_no_comment", "reject", "", "answered", "理由: 未入力", false},
		{"reject_whitespace", "reject", " \t", "answered", "理由: 未入力", false},
		{"text_only", "", "ええやん", "answered", "理由: ええやん", false},
		{"expired", "", "", "expired", "判断待ちが期限切れになりました (status: expired)", false},
		{"cancelled", "", "", "cancelled", "判断待ちがキャンセルされました (status: cancelled)", false},
		{"empty_answer", "", "", "answered", "判断待ちの回答が空です (status: answered)", false},
		{"whitespace_answer", "", " \t\n", "answered", "判断待ちの回答が空です (status: answered)", false},
		{"connection_failure", "", "", "error", "判断待ちのサーバーとの通信に失敗しました:", false},
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
			i := Input{Command: "npx", Args: []string{"wrangler", "deploy", "--env", "staging"}, Cwd: root, Env: EnvMap(env)}
			i.Env["AI_GUARD_REQUEST_REASON"] = "対象プロジェクトの確認用デプロイを行うため"
			approval := filepath.Join(root, "approvals")
			i.Env["AI_GUARD_TEMP_APPROVAL_FILE"] = approval
			verdict := Evaluate(Config{}, i)
			store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			await := func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
				if !p.AllowText || len(p.Options) != 3 || p.Options[0].ID != "approve" || p.Options[1].ID != "day_allow" || p.Options[2].ID != "reject" || p.TimeoutMS == nil || time.Duration(*p.TimeoutMS)*time.Millisecond != timeout {
					t.Fatalf("request contract changed: %+v", p)
				}
				if tc.status == "error" {
					conn, err := net.Dial("unix", filepath.Join(t.TempDir(), "x"))
					if conn != nil {
						conn.Close()
						t.Fatal("unexpected connection")
					}
					return api.Decision{}, err
				}
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
				default:
					answer := api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "mac-dialog"}
					if tc.option != "" {
						answer.OptionID = &tc.option
					}
					if tc.text != "" || tc.option == "" {
						answer.Text = &tc.text
					}
					return store.AnswerDecision(answer, now)
				}
			}
			var out bytes.Buffer
			got := Authorize(Config{}, root, i, "shim", &out, await)
			t.Logf("allowed=%v stderr=%q", got, out.String())
			if !tc.allowed && (!strings.Contains(out.String(), "承認が必要な理由: deploy/publish 系の操作は確認が必要です（規則: danger-word）") || !strings.Contains(out.String(), "引っかかったコマンド: npx wrangler deploy --env staging")) {
				t.Errorf("missing guard diagnostics: %s", out.String())
			}
			if got != tc.allowed || !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), "<nil>") {
				t.Errorf("allowed=%v stderr=%q; want allowed=%v contains=%q", got, out.String(), tc.allowed, tc.want)
			}
			if tc.allowed {
				if out.Len() != 0 {
					t.Errorf("approval output changed: %s", out.String())
				}
				key := verdict.TempKey
				if tc.option == "day_allow" {
					key = verdict.DayKey
				}
				if !validApproval(approval, key, time.Now()) {
					t.Errorf("approval missing for %s", key)
				}
				suppressed := "temporary"
				if tc.option == "day_allow" {
					suppressed = "day"
				}
				if v := Evaluate(Config{}, i); v.Decision != "allow" || v.Suppressed != suppressed {
					t.Errorf("approval no longer suppresses: %+v", v)
				}
			} else if _, err := os.Stat(approval); !os.IsNotExist(err) {
				t.Errorf("denied command wrote approval: %v", err)
			}
		})
	}
}

func TestApprovalPresentation(t *testing.T) {
	reason := "対象プロジェクト: ikesu。必要な理由: 表示を確認する。実施内容: パネルを検証する。確認済み: コマンドは実行しない。戻し方: パネルを閉じる。"
	for _, name := range []string{"AI_GUARD_REQUEST_REASON", "PROJECT_APPROVAL_REASON"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			a := NewAssets(root, "p1", "/usr/bin/true", Config{})
			env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Sync("codex"); err != nil {
				t.Fatal(err)
			}
			line := name + "='" + reason + "' bash /tmp/rebuild_all.sh && printf '%s' done"
			i := Input{Command: "bash", Args: []string{"/tmp/rebuild_all.sh"}, RawLine: line, Cwd: root, Env: EnvMap(env)}
			i.Env["AI_GUARD_REQUEST_ENV_NAME"] = name
			i.Env[name] = reason
			store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			await := func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
				want := "bash /tmp/rebuild_all.sh && printf '%s' done"
				if p.Title != "実行確認: "+want {
					t.Errorf("title=%q", p.Title)
				}
				body := "承認が必要な理由: 表示試験（規則: presentation-test）\n引っかかったコマンド: bash /tmp/rebuild_all.sh\n\nコマンド: " + want + "\n\n" + strings.ReplaceAll(reason, "。", "。\n") + "\n場所: " + root
				// Trailing prose punctuation does not add a blank line between fields.
				body = strings.ReplaceAll(body, "。\n\n場所:", "。\n場所:")
				if p.Body == nil || *p.Body != body {
					t.Errorf("body=%v want=%q", p.Body, body)
				}
				if p.Origin.Command != line || p.Origin.Reason != reason {
					t.Errorf("origin semantics changed: %+v", p.Origin)
				}
				d, err := store.CreateDecision(p, time.Now())
				if err != nil {
					return d, err
				}
				return store.CancelDecision(d.DecisionID, time.Now())
			}
			var out bytes.Buffer
			authorize(Config{}, root, i, "line", &out, await, &Verdict{Decision: "ask", Rule: "presentation-test", Reason: "表示試験"})
		})
	}
}

func TestApprovalTriggerForMultipleCommands(t *testing.T) {
	for _, separator := range []string{" | ", " && ", "; "} {
		t.Run(separator, func(t *testing.T) {
			root := t.TempDir()
			a := NewAssets(root, "p1", "/usr/bin/true", Config{})
			env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Sync("codex"); err != nil {
				t.Fatal(err)
			}
			i := Input{Cwd: root, Env: EnvMap(env)}
			i.Env["AI_GUARD_REQUEST_REASON"] = "対象プロジェクトの試験のため"
			line := "AI_GUARD_REQUEST_REASON='対象プロジェクトの試験のため' printf safe" + separator + "dd if=/dev/null"
			store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			called := false
			await := func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
				called = true
				prefix := "承認が必要な理由: 実行前に承認が必要です。（規則: dangerous）\n引っかかったコマンド: dd if=/dev/null\n\n"
				if p.Body == nil || !strings.HasPrefix(*p.Body, prefix) {
					t.Errorf("missing actual trigger: %+v", p.Body)
				}
				if p.Origin.Command != line {
					t.Errorf("origin changed: %q", p.Origin.Command)
				}
				d, err := store.CreateDecision(p, time.Now())
				if err != nil {
					return d, err
				}
				return store.CancelDecision(d.DecisionID, time.Now())
			}
			var out bytes.Buffer
			if AuthorizeLine(Config{}, root, i, line, &out, await) || !called {
				t.Fatalf("not requested: %s", out.String())
			}
		})
	}
}
