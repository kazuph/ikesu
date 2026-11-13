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
	for _, option := range []string{"approve", "day_allow"} {
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
	if len(asks) != 2 || asks[0].Tier != 3 || asks[0].AllowedBy != "approve" || asks[0].Dest != DestUnknown {
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

func answerAwait(t *testing.T, root, option string, asked *int) Await {
	store, err := dispatch.Open(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return func(p api.DecisionCreateParams, timeout time.Duration) (api.Decision, error) {
		*asked++
		d, err := store.CreateDecision(p, time.Now())
		if err != nil {
			return d, err
		}
		return store.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "test", OptionID: &option}, time.Now())
	}
}

func awsCall(root string, env map[string]string, sub string) Input {
	i := Input{Command: "aws", Args: []string{"dynamodb", sub, "--table-name", "t"}, Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "対象のテーブルを確認するため"
	return i
}

// 承認した1回だけ、別の段（シム）での同じコマンドの判定を通す。2回目は聞く。
func TestOneShotApprovalPassesSecondStageOnce(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	asked := 0
	await := answerAwait(t, root, "approve", &asked)
	// 1つ目の段（行の判定）で承認する。
	line := "aws dynamodb create-table --table-name t"
	i := awsCall(root, env, "create-table")
	if !AuthorizeLine(Config{}, root, i, line, &bytes.Buffer{}, await) || asked != 1 {
		t.Fatalf("first stage: asked=%d", asked)
	}
	// 2つ目の段（シムで argv が来る形）は聞かずに1回通る。
	if !Authorize(Config{}, root, awsCall(root, env, "create-table"), "shim", &bytes.Buffer{}, await) || asked != 1 {
		t.Fatalf("second stage was asked again: asked=%d", asked)
	}
	recs := readDecisionRecords(t, root)
	last := recs[len(recs)-1]
	if last.Decision != "allow" || last.AllowedBy != "one-shot" || last.Tier != TierL3 {
		t.Fatalf("one-shot record: %+v", last)
	}
	asks := readAskLines(t, root)
	if got := asks[len(asks)-1]; got.Outcome != OutcomeOneShot || got.AllowedBy != "one-shot" {
		t.Fatalf("one-shot ask record: %+v", got)
	}
	// 使ったあとの2回目は聞く。
	if !Authorize(Config{}, root, awsCall(root, env, "create-table"), "shim", &bytes.Buffer{}, await) || asked != 2 {
		t.Fatalf("third stage: asked=%d", asked)
	}
}

// 別の L3 のコマンド（create-table を承認して delete-table）・別の cwd は聞く。使わなかった許可は残る。
func TestOneShotApprovalDoesNotCoverOtherCommands(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	asked := 0
	await := answerAwait(t, root, "approve", &asked)
	if !Authorize(Config{}, root, awsCall(root, env, "create-table"), "shim", &bytes.Buffer{}, await) || asked != 1 {
		t.Fatal("approval failed")
	}
	if !Authorize(Config{}, root, awsCall(root, env, "delete-table"), "shim", &bytes.Buffer{}, await) || asked != 2 {
		t.Fatalf("delete-table passed on create-table's approval: asked=%d", asked)
	}
	other := t.TempDir()
	if !Authorize(Config{}, root, awsCall(other, env, "create-table"), "shim", &bytes.Buffer{}, await) || asked != 3 {
		t.Fatalf("another cwd passed: asked=%d", asked)
	}
	// 最初の承認は、使われなかったのでまだ残っている。
	if !Authorize(Config{}, root, awsCall(root, env, "create-table"), "shim", &bytes.Buffer{}, await) || asked != 3 {
		t.Fatalf("unused approval was lost: asked=%d", asked)
	}
}

// キーはペイン・シェルの PID・cwd・コマンドの文字列。30秒を過ぎると照合しない。1回照合したら消える。
func TestOneShotKeyAndExpiry(t *testing.T) {
	root := t.TempDir()
	m := Marker{Pane: "p1"}
	target := OneShotTarget{Cwd: "/work", Cmd: oneShotCommand("/usr/local/bin/aws", []string{"s3", "ls"})}
	t0 := time.Now()
	if err := writeOneShot(root, m, []OneShotTarget{target}, t0); err != nil {
		t.Fatal(err)
	}
	if target.Cmd != oneShotCommand("aws", []string{"s3", "ls"}) {
		t.Fatal("command string depends on how the command was spelled")
	}
	for name, other := range map[string]struct {
		m Marker
		t OneShotTarget
	}{
		"pane":    {Marker{Pane: "p2"}, target},
		"outside": {Marker{Pane: "p1", OutsidePane: true}, target},
		"cwd":     {m, OneShotTarget{Cwd: "/elsewhere", Cmd: target.Cmd}},
		"command": {m, OneShotTarget{Cwd: "/work", Cmd: oneShotCommand("aws", []string{"s3", "rm"})}},
	} {
		if takeOneShot(root, other.m, []OneShotTarget{other.t}, t0.Add(time.Second)) {
			t.Errorf("%s: matched a different key", name)
		}
	}
	saved := parentPID
	parentPID = func() int { return saved() + 1 }
	if takeOneShot(root, m, []OneShotTarget{target}, t0.Add(time.Second)) {
		t.Error("shell PID: matched a different shell")
	}
	parentPID = saved
	if takeOneShot(root, m, []OneShotTarget{target}, t0.Add(oneShotDuration+time.Second)) {
		t.Fatal("expired approval matched")
	}
	if err := writeOneShot(root, m, []OneShotTarget{target}, t0); err != nil {
		t.Fatal(err)
	}
	if !takeOneShot(root, m, []OneShotTarget{target}, t0.Add(oneShotDuration-time.Second)) {
		t.Fatal("approval within 30 seconds did not match")
	}
	if takeOneShot(root, m, []OneShotTarget{target}, t0.Add(time.Second)) {
		t.Fatal("approval matched twice")
	}
}

// 行全体を承認したときは、行の中の L3 のコマンドそれぞれの許可が書かれる。
func TestOneShotWholeLineWritesEachL3Command(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	asked := 0
	await := answerAwait(t, root, "approve", &asked)
	line := "aws dynamodb create-table --table-name t; aws dynamodb delete-table --table-name t"
	i := awsCall(root, env, "create-table")
	i.Command, i.Args = "", nil
	if !AuthorizeLine(Config{}, root, i, line, &bytes.Buffer{}, await) || asked != 1 {
		t.Fatalf("line approval: asked=%d", asked)
	}
	for _, sub := range []string{"create-table", "delete-table"} {
		if !Authorize(Config{}, root, awsCall(root, env, sub), "shim", &bytes.Buffer{}, await) || asked != 1 {
			t.Fatalf("%s was asked again: asked=%d", sub, asked)
		}
	}
	if !Authorize(Config{}, root, awsCall(root, env, "delete-table"), "shim", &bytes.Buffer{}, await) || asked != 2 {
		t.Fatalf("reuse: asked=%d", asked)
	}
}

// 一時許可・1日許可は L3 では今までどおり書かない。L1・L2 は使い切りの対象外。
func TestOneShotNotWrittenForL2(t *testing.T) {
	root, env := tierAuthorizeRoot(t)
	i := Input{Command: "curl", Args: []string{"-X", "POST", "https://example.com/x"}, Cwd: root, Env: env}
	i.Env["AI_GUARD_REQUEST_REASON"] = "外向きの確認のため"
	asked := 0
	if !Authorize(Config{}, root, i, "shim", &bytes.Buffer{}, answerAwait(t, root, "approve", &asked)) {
		t.Fatal("not approved")
	}
	if _, err := os.Stat(oneShotFile(root)); !os.IsNotExist(err) {
		t.Fatalf("one-shot file written for L2: %v", err)
	}
}
