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

func tierAuthorizeRoot(t *testing.T) (string, map[string]string) {
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
	t.Cleanup(func() { a.Close() })
	return root, EnvMap(env)
}

func outsideMarker(root string) *Marker {
	return &Marker{OutsidePane: true, Pane: "p1", Agent: "codex", Workspace: "w1", Cwd: root, Socket: filepath.Join(root, "socket"), Mode: "enforce", Policy: &Config{}}
}

func readDecisionRecords(t *testing.T, root string) []Record {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "guard", "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// ペインの中の L1 は、申請文なし・サーバーなしでも通り、取り消しも確認も出ない。
func TestAuthorizeL1AllowsInPaneWithoutReason(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "git", Args: []string{"clean", "-fd"}, Cwd: root, Env: env}
	var out bytes.Buffer
	if !Authorize(Config{}, root, i, "shim", &out, nil) {
		t.Fatalf("L1 not allowed in pane: %s", out.String())
	}
	if out.Len() != 0 {
		t.Fatalf("L1 printed output: %q", out.String())
	}
	asks := readAskLines(t, root)
	if len(asks) != 1 || asks[0].Outcome != OutcomeL1Allow || asks[0].Tier != 1 || asks[0].AllowedBy != "l1-default" || asks[0].Rule != "dangerous" {
		t.Fatalf("asks.jsonl: %+v", asks)
	}
	recs := readDecisionRecords(t, root)
	if len(recs) != 1 || recs[0].Decision != "allow" || recs[0].Tier != 1 || recs[0].AllowedBy != "l1-default" || recs[0].TierRule != "dangerous" {
		t.Fatalf("decisions.jsonl: %+v", recs)
	}
}

// ペインの外の L1 は今までどおり確認になる（申請文が無ければ取り消し）。
func TestAuthorizeL1OutsidePaneStillAsks(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "git", Args: []string{"clean", "-fd"}, Cwd: root, Env: env, VerifiedOutside: outsideMarker(root)}
	var out bytes.Buffer
	if Authorize(Config{}, root, i, "shim", &out, nil) {
		t.Fatal("L1 outside the pane was allowed")
	}
	if !strings.Contains(out.String(), "Command cancelled") {
		t.Fatalf("no cancellation: %s", out.String())
	}
	asks := readAskLines(t, root)
	if len(asks) != 1 || asks[0].Outcome != OutcomeNoReason || asks[0].Tier != 1 {
		t.Fatalf("asks.jsonl: %+v", asks)
	}
}

// 実行時の見張りは同じ行を何度も判定する。L1 の記録は60秒に1回だけ。
func TestAuthorizeL1RuntimeLineRecordedOncePerWindow(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Cwd: root, Env: env, RawLine: "git clean -fd", runtimeLine: true}
	v := askAt("dangerous", "実行前に承認が必要です。", TierL1, "")
	for n := 0; n < 3; n++ {
		if !authorize(Config{}, root, i, "line", &bytes.Buffer{}, nil, &v) {
			t.Fatal("L1 not allowed")
		}
	}
	if asks := readAskLines(t, root); len(asks) != 1 {
		t.Fatalf("runtime L1 records=%d", len(asks))
	}
	if recs := readDecisionRecords(t, root); len(recs) != 1 {
		t.Fatalf("runtime L1 decision records=%d", len(recs))
	}
	// 実行時でない行は毎回残す。
	i.runtimeLine = false
	if !authorize(Config{}, root, i, "line", &bytes.Buffer{}, nil, &v) || len(readAskLines(t, root)) != 2 {
		t.Fatal("non-runtime L1 was deduplicated")
	}
}

func cancelAwait(t *testing.T, root string, called *bool) Await {
	store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
		*called = true
		d, err := store.CreateDecision(p, time.Now())
		if err != nil {
			return d, err
		}
		return store.CancelDecision(d.DecisionID, time.Now())
	}
}

// L3 は一時許可・1日許可のファイルがあっても確認になる。環境変数で別のファイルを指しても読まない。
func TestAuthorizeL3IgnoresGrantsAndEnvironmentFile(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "aws", Args: []string{"s3", "ls"}, Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "対象のバケットを確認するため"
	v := Evaluate(Config{}, i)
	if v.Tier != TierL3 || v.TempKey == "" {
		t.Fatalf("precondition: %+v", v)
	}
	file := tempApprovalFile(root)
	for _, key := range []string{v.TempKey, v.DayKey} {
		if err := WriteApproval(file, key, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	if Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, cancelAwait(t, root, &called)) || !called {
		t.Fatalf("L3 was suppressed by grants (asked=%v)", called)
	}
}

func TestAuthorizeGrantFileComesFromStateRootNotEnvironment(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "npx", Args: []string{"wrangler", "deploy", "--env", "dev"}, Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "開発向けの確認デプロイを行うため"
	v := Evaluate(Config{}, i)
	if v.Tier != TierL2 || v.Dest != DestDev {
		t.Fatalf("precondition: %+v", v)
	}
	other := filepath.Join(root, "other-approvals")
	if err := WriteApproval(other, v.TempKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	i.Env["AI_GUARD_TEMP_APPROVAL_FILE"] = other
	i.Env["HOME"] = root
	if err := WriteApproval(filepath.Join(root, ".ai_guard_temp_approval"), v.TempKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	called := false
	if Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, cancelAwait(t, root, &called)) || !called {
		t.Fatalf("grant from the environment or the old file was read (asked=%v)", called)
	}
	// 状態のフォルダの下の記録は読む（L2 には1日許可・一時許可が効く）。
	if err := WriteApproval(tempApprovalFile(root), v.TempKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	called = false
	if !Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, cancelAwait(t, root, &called)) || called {
		t.Fatalf("grant under the state root was not applied (asked=%v)", called)
	}
	recs := readDecisionRecords(t, root)
	last := recs[len(recs)-1]
	if last.Decision != "allow" || last.AllowedBy != "temporary" || last.Tier != TierL2 || last.Dest != DestDev || last.TierRule != "danger-word" {
		t.Fatalf("grant record: %+v", last)
	}
}

// L3 は承認しても一時許可・1日許可を書かない。
func TestAuthorizeL3ApprovalWritesNoGrant(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "aws", Args: []string{"s3", "ls"}, Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "対象のバケットを確認するため"
	store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// L3 には1日許可の選択肢が無いので、答えようとしても受け付けられない。
	{
		var asked int
		option := "day_allow"
		await := answerAwaitOpts(t, root, option, &asked, nil)
		if Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, await) {
			t.Fatal("day_allow was accepted for L3")
		}
	}
	for _, option := range []string{"approve"} {
		option := option
		await := func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
			d, err := store.CreateDecision(p, time.Now())
			if err != nil {
				return d, err
			}
			return store.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "test", OptionID: &option}, time.Now())
		}
		if !Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, await) {
			t.Fatalf("%s not allowed", option)
		}
		if _, err := os.Stat(tempApprovalFile(root)); !os.IsNotExist(err) {
			t.Fatalf("L3 %s wrote a grant: %v", option, err)
		}
	}
	asks := readAskLines(t, root)
	if len(asks) != 2 || asks[1].Tier != 3 || asks[1].AllowedBy != "approve" || asks[1].Dest != DestUnknown || asks[0].Outcome != OutcomeNoServer {
		t.Fatalf("asks: %+v", asks)
	}
}

func TestAuditTierTable(t *testing.T) {
	now := time.Now()
	mk := func(tier int, rule, agent, ws, outcome string) AskRecord {
		return AskRecord{TSEpoch: now.Add(-time.Hour).Unix(), TS: now.Add(-time.Hour).UTC().Format(time.RFC3339), Rule: rule, Command: rule + " x", Tier: tier, Agent: agent, Workspace: ws, Outcome: outcome}
	}
	rep, err := BuildAudit(AuditOptions{Now: now, Since: 24 * time.Hour, Asks: []AskRecord{
		mk(1, "dangerous", "codex", "w1", OutcomeL1Allow), mk(1, "dangerous", "codex", "w1", OutcomeL1Allow),
		mk(2, "danger-word", "claude", "w1", OutcomeApprove), mk(3, "aws", "claude", "w2", OutcomeReject),
		mk(0, "aws", "claude", "", OutcomeApprove),
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"L1": 2, "L2": 1, "L3": 1, TierUnknown: 1}
	for k, n := range want {
		if rep.TierTotals[k] != n {
			t.Fatalf("totals=%v", rep.TierTotals)
		}
	}
	found := false
	for _, r := range rep.TierTable {
		if r.Tier == "L1" && r.Rule == "dangerous" && r.Agent == "codex" && r.Workspace == "w1" && r.Outcome == OutcomeL1Allow && r.Count == 2 {
			found = true
		}
	}
	if !found || len(rep.TierTable) != 4 {
		t.Fatalf("table=%+v", rep.TierTable)
	}
	var text, js bytes.Buffer
	RenderAuditText(&text, rep)
	if err := RenderAuditJSON(&js, rep); err != nil {
		t.Fatal(err)
	}
	html := RenderAuditHTML(rep)
	for _, s := range []string{"L1 2 / L2 1 / L3 1 / " + TierUnknown + " 1"} {
		if !strings.Contains(text.String(), s) || !strings.Contains(html, s) {
			t.Fatalf("tier totals missing: %s", text.String())
		}
	}
	if !strings.Contains(js.String(), `"tier_table"`) || !strings.Contains(js.String(), `"tier_totals"`) {
		t.Fatalf("json: %s", js.String())
	}
	for _, rule := range []string{"d1-sql-write", "dangerous", "git-alias", "wide-scan"} {
		if _, ok := auditRuleKinds[rule]; !ok {
			t.Errorf("rule kind missing: %s", rule)
		}
	}
}

// 一時許可は、行の中の ask 全部に有効な許可があるときだけ当てる。
func TestTemporaryGrantMustCoverEveryAskInLine(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	reason := "外向きの確認を行うため"
	key := func(cmd string, args ...string) string {
		v := Evaluate(Config{}, Input{Command: cmd, Args: args, Cwd: root, Env: env})
		if v.Decision != "ask" || v.TempKey == "" {
			t.Fatalf("%s %v: %+v", cmd, args, v)
		}
		return v.TempKey
	}
	curlKey := key("curl", "-X", "POST", "https://example.com/a")
	ghKey := key("gh", "workflow", "run", "ci.yml")
	if err := WriteApproval(tempApprovalFile(root), curlKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"curl -X POST https://example.com/a; gh workflow run ci.yml",
		"gh workflow run ci.yml; curl -X POST https://example.com/a",
	} {
		i := Input{Cwd: root, Env: env}
		i.Env["AI_GUARD_REQUEST_REASON"] = reason
		asked := false
		if AuthorizeLine(Config{}, root, i, line, &bytes.Buffer{}, cancelAwait(t, root, &asked)) || !asked {
			t.Fatalf("%q was allowed by a grant that covers only one of its asks", line)
		}
	}
	// 1つだけの行は今までどおり許可で通る。
	i := Input{Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = reason
	asked := false
	if !AuthorizeLine(Config{}, root, i, "curl -X POST https://example.com/a", &bytes.Buffer{}, cancelAwait(t, root, &asked)) || asked {
		t.Fatal("single-ask line with a grant was asked")
	}
	// 両方に許可があれば通る。
	if err := WriteApproval(tempApprovalFile(root), ghKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	asked = false
	if !AuthorizeLine(Config{}, root, i, "gh workflow run ci.yml; curl -X POST https://example.com/a", &bytes.Buffer{}, cancelAwait(t, root, &asked)) || asked {
		t.Fatal("line whose asks all have grants was asked")
	}
}

// 行を承認すると、行の中の ask 全部の許可が書かれる。
func TestApprovalWritesGrantForEveryAskInLine(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "外向きの確認を行うため"
	line := "curl -X POST https://example.com/a; gh workflow run ci.yml"
	asked := 0
	if !AuthorizeLine(Config{}, root, i, line, &bytes.Buffer{}, answerAwaitOpts(t, root, "approve", &asked, nil)) || asked != 1 {
		t.Fatal("not approved")
	}
	for _, args := range [][]string{{"curl", "-X", "POST", "https://example.com/a"}, {"gh", "workflow", "run", "ci.yml"}} {
		v := Evaluate(Config{}, Input{Command: args[0], Args: args[1:], Cwd: root, Env: env})
		if !validApproval(tempApprovalFile(root), v.TempKey, time.Now()) {
			t.Fatalf("grant missing for %v", args)
		}
	}
}

func answerAwaitOpts(t *testing.T, root, option string, asked *int, got *[]api.DecisionOption) Await {
	store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
		*asked++
		if got != nil {
			*got = p.Options
		}
		d, err := store.CreateDecision(p, time.Now())
		if err != nil {
			return d, err
		}
		return store.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "test", OptionID: &option}, time.Now())
	}
}

// L3 の確認には1日許可の選択肢を出さない。L2 には出す。
func TestL3AskHasNoDayAllowOption(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	ids := func(opts []api.DecisionOption) string {
		var out []string
		for _, o := range opts {
			out = append(out, o.ID)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		name string
		in   Input
		want string
	}{
		{"L3", Input{Command: "aws", Args: []string{"s3", "ls"}, Cwd: root, Env: env}, "approve,reject"},
		{"L2", Input{Command: "curl", Args: []string{"-X", "POST", "https://example.com/x"}, Cwd: root, Env: env}, "approve,day_allow,reject"},
	} {
		tc.in.Env["AI_GUARD_REQUEST_REASON"] = "確認のため"
		var got []api.DecisionOption
		asked := 0
		if !Authorize(Config{}, root, tc.in, "shim", &bytes.Buffer{}, answerAwaitOpts(t, root, "approve", &asked, &got)) {
			t.Fatalf("%s not approved", tc.name)
		}
		if ids(got) != tc.want {
			t.Errorf("%s options=%s want %s", tc.name, ids(got), tc.want)
		}
	}
}

// L1 の経路でも、記録できないときは今までと同じく止める。
func TestL1StopsWhenLogFails(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "guard", "decisions.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	i := Input{Command: "git", Args: []string{"clean", "-fd"}, Cwd: root, Env: env}
	var out bytes.Buffer
	if Authorize(Config{}, root, i, "shim", &out, nil) {
		t.Fatalf("L1 allowed although the log could not be written: %s", out.String())
	}
	if !strings.Contains(out.String(), "guard log:") {
		t.Fatalf("no log error: %s", out.String())
	}
}

// 聞かずに通した L1 は、今までの件数・束・分類に入れず、ティアの表だけで数える。
func TestAuditExcludesL1AllowFromTotalsAndBundles(t *testing.T) {
	now := time.Now()
	mk := func(tier int, rule, outcome string) AskRecord {
		return AskRecord{TSEpoch: now.Add(-time.Hour).Unix(), TS: now.Add(-time.Hour).UTC().Format(time.RFC3339), Rule: rule, Command: rule + " x", Tier: tier, Outcome: outcome}
	}
	rep, err := BuildAudit(AuditOptions{Now: now, Since: 24 * time.Hour, Asks: []AskRecord{
		mk(1, "dangerous", OutcomeL1Allow), mk(1, "dangerous", OutcomeL1Allow), mk(3, "aws", OutcomeApprove),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 1 || rep.Outcomes[OutcomeL1Allow] != 0 || rep.BundleCount != 1 || len(rep.Top) != 1 || rep.Top[0].Rule != "aws" {
		t.Fatalf("l1_allow leaked into totals: %+v", rep)
	}
	if rep.TierTotals["L1"] != 2 || rep.TierTotals["L3"] != 1 {
		t.Fatalf("tier table: %+v", rep.TierTotals)
	}
}
