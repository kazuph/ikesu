package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
				want += "   判定理由: 実行前に承認が必要です。\n"
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
