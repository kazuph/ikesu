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

func presentationFixture(t *testing.T, command string, args []string, raw string, extras map[string]string) (api.DecisionCreateParams, string, string) {
	t.Helper()
	root := t.TempDir()
	a := NewAssets(root, "p1", "/usr/bin/true", Config{})
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	i := Input{Command: command, Args: args, RawLine: raw, Cwd: root, Env: EnvMap(env)}
	i.Env["AI_GUARD_REQUEST_REASON"] = "対象プロジェクト: ikesu。必要な理由: 表示試験。実施内容: 実行せず取り消す。確認済み: 試験用。戻し方: 中止。"
	for k, v := range extras {
		i.Env[k] = v
	}
	store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var captured api.DecisionCreateParams
	var out bytes.Buffer
	Authorize(Config{}, root, i, "shim", &out, func(p api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
		captured = p
		d, e := store.CreateDecision(p, time.Now())
		if e != nil {
			return d, e
		}
		return store.CancelDecision(d.DecisionID, time.Now())
	})
	data, err := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return captured, out.String(), string(data)
}

func TestGuardPresentationDeduplicatesAndRedacts(t *testing.T) {
	raw := "curl -X POST https://api.example.test -H 'Authorization: Bearer sk-proj-TESTONLY'"
	for _, pipeline := range []bool{false, true} {
		line := raw
		if pipeline {
			line += " | cat"
		}
		p, out, record := presentationFixture(t, "curl", []string{"-X", "POST", "https://api.example.test", "-H", "Authorization: Bearer sk-proj-TESTONLY"}, line, nil)
		encoded, _ := json.Marshal(p)
		for name, value := range map[string]string{"decision": string(encoded), "feedback": out, "record": record} {
			if strings.Contains(value, "sk-proj-TESTONLY") {
				t.Errorf("%s exposed fixture credential", name)
			}
		}
		if p.Body == nil {
			t.Fatal("missing decision")
		}
		if !pipeline && strings.Count(*p.Body, "curl -X POST") != 1 {
			t.Errorf("command repeated: %s", *p.Body)
		}
		if !pipeline && strings.Contains(*p.Body, "対象コマンド：") {
			t.Error("whole command repeated as target")
		}
		if pipeline && !strings.Contains(*p.Body, "対象コマンド：") {
			t.Error("distinct pipeline trigger omitted")
		}
		if strings.Contains(p.Title, "-X POST") {
			t.Error("full command repeated in title")
		}
		if strings.Contains(*p.Body, "場所:") {
			t.Error("cwd repeated beside origin")
		}
		if !strings.Contains(out, "ブロック理由：") || !strings.Contains(out, "対象コマンド：") {
			t.Error("AI diagnostics lost")
		}
	}
}

func TestNestedGuardRequestsMissingOrInheritedReason(t *testing.T) {
	for _, reason := range []string{"", "English only request", "外側からの試験申請文"} {
		p, out, _ := presentationFixture(t, "curl", []string{"-X", "POST", "https://api.example.test"}, "", map[string]string{
			"AI_GUARD_REQUEST_REASON": "", "IKESU_GUARD_PARENT_COMMAND": "bash img.sh", "IKESU_GUARD_PARENT_REASON": reason,
		})
		if !ValidReason(reason) {
			if p.Body != nil || !strings.Contains(out, "外側のコマンド: bash img.sh") {
				t.Fatal("invalid nested reason did not stop before dialog", out)
			}
			continue
		}
		if p.Body == nil {
			t.Fatal("valid inherited reason did not create dialog", out)
		}
		if reason != "" && (!strings.Contains(*p.Body, "外側のコマンド bash img.sh から引き継ぎ") || !strings.Contains(*p.Body, reason)) {
			t.Error("inherited request was not explained")
		}

	}
}

func TestSecretFormatsAreHiddenWithoutChangingArgvShape(t *testing.T) {
	fixtures := []string{
		"Authorization: Bearer opaque-test-credential", "sk-proj-TESTONLY", "sk-TESTONLY", "xoxb-TESTONLY", "xoxp-TESTONLY", "ghp_TESTONLY", "github_pat_TESTONLY", "AKIATESTONLY",
		"OPENAI_API_KEY='test value'", "SERVICE_TOKEN=test-value", "APP_SECRET=test-value", "PASSWORD=hidden", "https://user:pass@example.test/path",
	}
	for _, fixture := range fixtures {
		got := redact(fixture)
		if !strings.Contains(got, "***") || got == fixture {
			t.Error("secret fixture was not redacted")
		}
	}
	argv := []string{"curl", "-H", "Authorization:", "Bearer", "opaque-test-credential"}
	got := redactArgv(argv)
	if len(got) != len(argv) || strings.Contains(strings.Join(got, " "), "opaque-test-credential") {
		t.Fatal("split authorization header leaked or argv shape changed")
	}
	if got := redact("curl -X POST https://example.test"); got != "curl -X POST https://example.test" {
		t.Fatal("ordinary command changed")
	}
}
