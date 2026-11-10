package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

const unitsReason = "対象プロジェクト: ikesu。必要な理由: 行ごとの表示の確認。実施内容: 実行せず却下。"

// unitsDecision は AuthorizeLine が作る承認ダイアログの申請を取り出す（コマンドは実行しない）。
func unitsDecision(t *testing.T, i Input, line string) *api.DecisionCreateParams {
	t.Helper()
	i.Env["AI_GUARD_REQUEST_REASON"] = unitsReason
	var got *api.DecisionCreateParams
	var out strings.Builder
	allowed := AuthorizeLine(Config{}, t.TempDir(), i, line, &out, func(p api.DecisionCreateParams, _ time.Duration) (api.Decision, error) {
		got = &p
		return api.Decision{Status: "cancelled"}, nil
	})
	if allowed {
		t.Fatalf("実行された: %s", line)
	}
	if got == nil || got.Origin == nil {
		t.Fatalf("申請が無い: %s", out.String())
	}
	return got
}

func unitsWrite(t *testing.T, i Input, name, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(i.Cwd, name), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestCommandUnitsScriptLine(t *testing.T) {
	i, _ := scriptPane(t)
	if e := os.Mkdir(filepath.Join(i.Cwd, "sub"), 0700); e != nil {
		t.Fatal(e)
	}
	unitsWrite(t, i, "a.py", "print(1)\n")
	unitsWrite(t, i, "s.sh", "echo hi\n/bin/rm /outside/victim\n")
	p := unitsDecision(t, i, "cd sub && python3 a.py && bash ../s.sh | tail -5")
	u := p.Origin.CommandUnits
	want := []struct{ op, text, label, verdict string }{
		{"", "cd sub", "移動", "allow"},
		{"&&", "python3 a.py", "実行", "allow"},
		{"&&", "bash ../s.sh", "実行", "ask"},
		{"|", "tail -5", "読む", "allow"},
	}
	if len(u) != len(want) {
		t.Fatalf("行数 %d: %+v", len(u), u)
	}
	for n, w := range want {
		if u[n].Op != w.op || u[n].Text != w.text || u[n].Label != w.label || u[n].Verdict != w.verdict {
			t.Errorf("行%d: got %+v want %+v", n, u[n], w)
		}
	}
	if !strings.Contains(u[2].Target, "rm") || u[2].Rule == "" || u[2].Reason == "" {
		t.Errorf("引っかかった行に Target・規則・理由が無い: %+v", u[2])
	}
	for n, x := range u {
		if n != 2 && (x.Target != "" || x.Rule != "") {
			t.Errorf("他の行に判定が漏れた: %+v", x)
		}
	}
	if p.Origin.GuardRule == "" || p.Origin.GuardReason == "" {
		t.Errorf("GuardRule・GuardReason が無い: %+v", p.Origin)
	}
	if p.Origin.Command != "cd sub && python3 a.py && bash ../s.sh | tail -5" {
		t.Errorf("Command が変わった: %q", p.Origin.Command)
	}
}

func TestCommandUnitsHeredocAndNewline(t *testing.T) {
	i, _ := scriptPane(t)
	p := unitsDecision(t, i, "cat <<EOF\nline1\nline2\nEOF\n/bin/rm /outside/victim")
	u := p.Origin.CommandUnits
	if len(u) != 2 {
		t.Fatalf("%+v", u)
	}
	if u[0].Text != "cat <<EOF ＋here-doc 2行" || u[0].Label != "読む" || u[0].Verdict != "allow" {
		t.Errorf("here-doc: %+v", u[0])
	}
	if u[1].Op != "\n" || u[1].Text != "/bin/rm /outside/victim" || u[1].Label != "消す" || u[1].Verdict != "ask" {
		t.Errorf("rm: %+v", u[1])
	}
	p = unitsDecision(t, i, "echo a; /bin/rm /outside/victim")
	if u := p.Origin.CommandUnits; len(u) != 2 || u[1].Op != ";" {
		t.Errorf("; の区切り: %+v", u)
	}
}

func TestCommandUnitsRedaction(t *testing.T) {
	i, _ := scriptPane(t)
	p := unitsDecision(t, i, "TOKEN=abc curl -X POST https://x.example/send && /bin/rm /outside/victim")
	u := p.Origin.CommandUnits
	if len(u) != 2 {
		t.Fatalf("%+v", u)
	}
	if u[0].Label != "外へ送る" {
		t.Errorf("札: %+v", u[0])
	}
	for _, x := range u {
		if strings.Contains(x.Text+x.Target+x.Reason, "abc") {
			t.Errorf("秘密が漏れた: %+v", x)
		}
	}
	p = unitsDecision(t, i, "printf ok; curl -X POST https://example.test/send -F 'PASSWORD=SECRET_HEAD_TESTONLY'; /bin/rm /outside/victim")
	for _, x := range p.Origin.CommandUnits {
		if strings.Contains(x.Text+x.Target+x.Reason, "SECRET_HEAD_TESTONLY") {
			t.Errorf("秘密が漏れた: %+v", x)
		}
	}
}

func TestCommandUnitsLineWideVerdictHasNoAskLine(t *testing.T) {
	i, _ := scriptPane(t)
	// 前置きの引数を確定できない行は、行全体の判定（shell-line）で ask になる。どの行も ask にしない。
	line := "nice -z ls && echo a"
	out, v, _ := lineVerdict(Config{}, i, line)
	if v.Rule != "shell-line" {
		t.Fatalf("行全体の判定にならない: %+v", v)
	}
	u := out.commandUnits(line)
	if len(u) != 2 {
		t.Fatalf("%+v", u)
	}
	for _, x := range u {
		if x.Verdict != "allow" {
			t.Errorf("行全体の判定なのに行が %s: %+v", x.Verdict, x)
		}
	}
}

func TestCommandUnitsUnjudgedAfterBlock(t *testing.T) {
	i, _ := scriptPane(t)
	line := "echo a && git push --force origin main && echo b"
	_, v, _ := lineVerdict(Config{}, i, line)
	if v.Decision != "block" {
		t.Fatalf("block にならない: %+v", v)
	}
	out, _, _ := lineVerdict(Config{}, i, line)
	u := out.commandUnits(line)
	if len(u) != 3 || u[1].Verdict != "block" || u[2].Verdict != "unjudged" || u[0].Verdict != "allow" {
		t.Errorf("%+v", u)
	}
}

func TestCommandUnitsBestLineIsAsk(t *testing.T) {
	i, _ := scriptPane(t)
	for _, line := range []string{
		"echo a && /bin/rm /outside/victim",
		"/bin/rm /outside/victim; echo a",
		"echo a | tail -1 && cat x || /bin/rm /outside/victim",
	} {
		out, v, _ := lineVerdict(Config{}, i, line)
		if v.Decision != "ask" {
			t.Fatalf("%s: %+v", line, v)
		}
		bestAt := strings.Index(line, "/bin/rm")
		found := false
		for _, e := range out.judgement.entries {
			if e.offset == bestAt && e.v.Decision == "ask" {
				found = true
			}
		}
		if !found {
			t.Errorf("best の命令の行が ask でない: %s %+v", line, out.judgement.entries)
		}
		for _, x := range out.commandUnits(line) {
			if strings.Contains(x.Text, "/bin/rm") && x.Verdict != "ask" {
				t.Errorf("%s: %+v", line, x)
			}
		}
	}
}

func TestCommandLabels(t *testing.T) {
	for line, want := range map[string]string{
		"git status": "読む", "git -C /x log": "読む", "git commit -m x": "書く", "git reset --hard": "消す", "git clean -fd": "消す", "git push": "外へ送る",
		"sudo rm -rf x": "消す", "env A=1 nice -n 5 cat x": "読む", "echo hi > out.txt": "書く", "echo hi 2>/dev/null": "読む",
		"FOO=1": "書く", "$CMD x": "不明", "ssh host ls": "別マシン", "scp a h:/b": "別マシン", "scp a b": "実行",
		"curl https://x": "実行", "curl -d a https://x": "外へ送る", "sed -i s/a/b/ f": "書く", "sed s/a/b/ f": "実行",
		"gh pr list": "読む", "gh pr create": "外へ送る", "gh pr merge 1": "外へ送る", "gh issue view 3": "読む", "gh run watch 1": "読む", "gh repo view": "読む", "gh release list": "読む", "gh search prs x": "読む", "gh auth status": "読む",
		"gh api repos/a/b": "読む", "gh api -X GET repos/a/b": "読む", "gh api -X POST repos/a/b": "外へ送る", "gh api repos/a/b -f x=1": "外へ送る", "gh api --input f repos/a": "外へ送る",
		"find . -name x": "読む", "find . -delete": "消す", "find . -exec rm {} ;": "消す", "find . -execdir trash {} ;": "消す", "find . -exec cat {} ;": "読む",
		"git branch -D x": "消す", "git branch --delete x": "消す", "git branch -d x": "消す", "git stash drop": "消す", "git stash clear": "消す", "git stash": "書く", "git -C /x branch -D y": "消す", "git -C /x stash drop": "消す", "git fetch": "読む", "npm publish": "外へ送る", "npm test": "実行", "docker push x": "外へ送る", "make": "実行",
	} {
		u, ok := splitUnits(line)
		if !ok || len(u) != 1 || u[0].label != want {
			t.Errorf("%q: got %+v want %s", line, u, want)
		}
	}
}
