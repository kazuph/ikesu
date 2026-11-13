package guard

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// lineClaimsOf は lineVerdict の後の控えから、ask の命令ごとの claim（空は作れなかったもの）を返す。
func lineClaimsOf(t *testing.T, i Input, line string) (entries []judgeEntry) {
	t.Helper()
	out, _, _ := lineVerdict(Config{}, i, line)
	if out.judgement == nil {
		t.Fatalf("no judgement for %q", line)
	}
	for _, e := range out.judgement.entries {
		if e.v.Decision == "ask" {
			entries = append(entries, e)
		}
	}
	return entries
}

func claimOf(t *testing.T, cwd, command string, args ...string) string {
	t.Helper()
	h, ok := lineClaimHash(Input{Command: command, Args: args, Cwd: cwd})
	if !ok {
		t.Fatalf("no claim for %s %v", command, args)
	}
	return h
}

// SPEC-line-approval.md 6章 (a): claim の単体
func TestLineClaimTable(t *testing.T) {
	i, r, _ := tierPane(t, true)
	if err := os.WriteFile(filepath.Join(r, "x.sh"), []byte("aws s3 rb s3://b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	aws := claimOf(t, r, "aws", "dynamodb", "delete-table", "--table-name", "t")

	single := func(line string, want string) {
		t.Helper()
		es := lineClaimsOf(t, i, line)
		if len(es) != 1 || es[0].claim != want {
			t.Errorf("%q: entries=%d claims=%v want %s", line, len(es), claimsOf(es), want)
		}
	}
	single(`aws dynamodb delete-table --table-name t`, aws)
	single(`command aws dynamodb delete-table --table-name t`, aws)
	single(`nohup nice -n 5 aws dynamodb delete-table --table-name t`, aws)
	single(`cd /tmp && aws s3 rb s3://b`, claimOf(t, physical("/tmp"), "aws", "s3", "rb", "s3://b"))
	single(`/opt/homebrew/bin/aws s3 rb s3://b`, claimOf(t, r, "/opt/homebrew/bin/aws", "s3", "rb", "s3://b"))
	single(`sudo diskutil eraseDisk JHFS+ x disk9`, claimOf(t, r, "sudo", "diskutil", "eraseDisk", "JHFS+", "x", "disk9"))

	// 同じ claim が2つ（送るのは1件）
	es := lineClaimsOf(t, i, `aws s3 rb s3://b & aws s3 rb s3://b; wait`)
	if len(es) != 2 || es[0].claim == "" || es[0].claim != es[1].claim {
		t.Errorf("duplicate claims: %v", claimsOf(es))
	}
	if got := lineClaims(&lineJudgement{entries: es}); len(got) != 1 {
		t.Errorf("sent claims=%d want 1", len(got))
	}

	// 2件
	es = lineClaimsOf(t, i, `curl -X DELETE https://example.com/a; gh repo delete x/y --yes`)
	if len(es) != 2 || es[0].claim == "" || es[1].claim == "" || es[0].claim == es[1].claim {
		t.Errorf("two commands: %v", claimsOf(es))
	}

	// スクリプトを走らせる: [bash, x.sh]＋ファイルの身元の claim と、中の aws の claim
	es = lineClaimsOf(t, i, `bash x.sh`)
	if len(es) != 1 || es[0].claim == "" || len(es[0].scriptClaims) != 1 || es[0].scriptClaims[0] != claimOf(t, r, "aws", "s3", "rb", "s3://b") {
		t.Fatalf("script claims: %+v", es)
	}
	before := es[0].claim
	if err := os.WriteFile(filepath.Join(r, "x.sh"), []byte("aws s3 rb s3://other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if again := lineClaimsOf(t, i, `bash x.sh`); len(again) != 1 || again[0].claim == before {
		t.Error("changing the script did not change its claim")
	}

	// 作らない: 未確定の語・ループ・サブシェルの cd
	for _, line := range []string{
		`aws s3 rb "s3://$B"`,
		`for t in a b; do aws dynamodb delete-table --table-name $t; done`,
		`(cd /tmp && aws s3 rb s3://b)`,
	} {
		for _, e := range lineClaimsOf(t, i, line) {
			if e.claim != "" {
				t.Errorf("%q made a claim", line)
			}
		}
	}

	// 引数は配列のまま入れる: 空白でつながない
	a := claimOf(t, r, "rm", "-rf", "/a b")
	b := claimOf(t, r, "rm", "-rf", "/a", "b")
	if a == b {
		t.Error("argv was joined with spaces")
	}
	// コマンド名が空・未確定・cd 後で cwd が決まらない入力には作らない
	for _, in := range []Input{{Cwd: r}, {Command: "aws", Cwd: r, Unresolved: true}, {Command: "aws", Cwd: r, CwdUncertain: true}} {
		if _, ok := lineClaimHash(in); ok {
			t.Errorf("claim for %+v", in)
		}
	}
	if _, ok := lineClaimHash(Input{Command: "bash", Args: []string{"missing.sh"}, Cwd: r}); ok {
		t.Error("claim although the script cannot be read")
	}
}

func claimsOf(es []judgeEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.claim)
	}
	sort.Strings(out)
	return out
}

// 2段目以降の照合: 承認された claim の命令だけ通し、別の L3・claim の無い命令・行全体のエラーは聞く。
func TestLineApprovedOnLaterStages(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	reason := "対象のテーブルを確認するため"
	approved := map[string]bool{}
	fake := func(hash, rule string) bool { return rule == "line-approved" && approved[hash] }
	create := claimOf(t, root, "aws", "dynamodb", "create-table", "--table-name", "t")
	del := claimOf(t, root, "aws", "dynamodb", "delete-table", "--table-name", "t")
	approved[create] = true

	asked := func(run func(Await) bool) (allowed, wasAsked bool) {
		var called bool
		allowed = run(cancelAwait(t, root, &called))
		return allowed, called
	}
	shim := func(sub string) func(Await) bool {
		return func(a Await) bool {
			i := Input{Command: "aws", Args: []string{"dynamodb", sub, "--table-name", "t"}, Cwd: root, Env: env, ApprovedInvocation: fake}
			i.Env["AI_GUARD_REQUEST_REASON"] = reason
			return Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, a)
		}
	}
	line := func(text string) func(Await) bool {
		return func(a Await) bool {
			i := Input{Cwd: root, Env: env, ApprovedInvocation: fake}
			i.Env["AI_GUARD_REQUEST_REASON"] = reason
			return AuthorizeLine(Config{}, root, i, text, &bytes.Buffer{}, a)
		}
	}
	// シム・関数: 自分の claim が承認済みなら通す（記録の通した理由は outer-command）。別の L3 は聞く。
	if ok, was := asked(shim("create-table")); !ok || was {
		t.Fatalf("approved claim was asked: allowed=%v asked=%v", ok, was)
	}
	if recs := readDecisionRecords(t, root); recs[len(recs)-1].AllowedBy != "outer-command" {
		t.Fatalf("record: %+v", recs[len(recs)-1])
	}
	if ok, was := asked(shim("delete-table")); ok || !was {
		t.Fatalf("create-table's approval covered delete-table: allowed=%v asked=%v", ok, was)
	}
	// 行: ask の命令の全部が承認済みのときだけ通す。
	if ok, was := asked(line("aws dynamodb create-table --table-name t")); !ok || was {
		t.Fatalf("approved line was asked: %v %v", ok, was)
	}
	if ok, was := asked(line("aws dynamodb create-table --table-name t; aws dynamodb delete-table --table-name t")); ok || !was {
		t.Fatalf("line with an unapproved ask passed: %v %v", ok, was)
	}
	approved[del] = true
	if ok, was := asked(line("aws dynamodb create-table --table-name t; aws dynamodb delete-table --table-name t")); !ok || was {
		t.Fatalf("line whose asks are all approved was asked: %v %v", ok, was)
	}
	// claim の作れない命令（未確定の語）は通さない。
	if ok, was := asked(line(`aws dynamodb create-table --table-name "$T"`)); ok || !was {
		t.Fatalf("unresolved command passed: %v %v", ok, was)
	}
	// 行全体のエラー（shell-line）は通さない。
	if ok, was := asked(line("aws dynamodb create-table --table-name t; export BASH_ENV=/tmp/x")); ok || !was {
		t.Fatalf("shell-line passed: %v %v", ok, was)
	}
}

// 1段目の確認に付ける claim は、claude-prefix と guard line の1段目の --via のときだけ。guard exec は付けない。
func TestLineApprovalClaimsOnlyFromFirstStages(t *testing.T) {
	i, _, _ := tierPane(t, true)
	out, _, _ := lineVerdict(Config{}, i, "aws dynamodb delete-table --table-name t")
	for via, want := range map[string]int{"claude-prefix": 1, "accept-line": 1, "exec-string": 1, "zsh-debug": 1, "bash-debug": 1, "": 0, "shim": 0, "shell-func": 0} {
		out.LineVia = via
		if got := len(lineApprovalClaims(out)); got != want {
			t.Errorf("via=%q claims=%d want %d", via, got, want)
		}
	}
}
