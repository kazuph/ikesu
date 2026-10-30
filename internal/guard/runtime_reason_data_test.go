package guard

import (
	"strings"
	"testing"
)

func TestRuntimeReasonNameInCurlData(t *testing.T) {
	i := WithShellVariables(Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin"}}, strings.NewReader("declare -- CCORD_WEBHOOK_URL=\"https://example.test/hook\"\ndeclare -- INPUT='{\"response\":\"AI_GUARD_REQUEST_REASON\"}'\n"))
	line := `curl -s -X POST "$CCORD_WEBHOOK_URL" -H "Content-Type: application/json" -d "$INPUT" --connect-timeout 2 --max-time 5 > /dev/null 2>&1`
	_, got, _ := lineVerdict(Config{}, i, line)
	if got.Rule == "shell-line" && strings.Contains(got.Reason, "申請文の名前を含む操作") {
		t.Fatalf("data treated as mutation: %+v", got)
	}
	if got.Decision != "ask" || got.Rule != "http-write" {
		t.Fatalf("HTTP policy changed: %+v", got)
	}
	for _, url := range []string{"http://127.0.0.1:1234/hook", "https://example.test/hook"} {
		verdicts := make([]Verdict, 0, 2)
		for _, data := range []string{"ordinary text", "AI_GUARD_REQUEST_REASON"} {
			snapshot := "declare -- CCORD_WEBHOOK_URL='" + url + "'\ndeclare -- INPUT='" + data + "'\n"
			input := WithShellVariables(Input{Cwd: i.Cwd, Env: i.Env}, strings.NewReader(snapshot))
			_, v, _ := lineVerdict(Config{}, input, line)
			verdicts = append(verdicts, v)
		}
		if verdicts[0].Decision != verdicts[1].Decision || verdicts[0].Rule != verdicts[1].Rule {
			t.Errorf("%s: data changed policy: %+v / %+v", url, verdicts[0], verdicts[1])
		}
	}
}
