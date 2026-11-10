package guard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

var auditNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)

func ask(rule, cmd, outcome string, ago time.Duration, mod ...func(*AskRecord)) AskRecord {
	ts := auditNow.Add(-ago)
	r := AskRecord{TS: ts.UTC().Format(time.RFC3339), TSEpoch: ts.Unix(), Rule: rule, Command: cmd, Cwd: "/work/app", Repo: "/work/app", Outcome: outcome, WaitMS: 3000}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func auditOf(t *testing.T, asks []AskRecord, decisions ...api.Decision) *AuditReport {
	t.Helper()
	rep, err := BuildAudit(AuditOptions{Now: auditNow, Since: 7 * 24 * time.Hour, Top: 10, Asks: asks, Decisions: decisions})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func findBundle(t *testing.T, rep *AuditReport, rule, cmd string) Bundle {
	t.Helper()
	for _, b := range rep.Top {
		if b.Rule == rule && b.Command == cmd {
			return b
		}
	}
	t.Fatalf("bundle %s/%s not in %+v", rule, cmd, rep.Top)
	return Bundle{}
}

// 6つの分類をそれぞれ1例ずつ。
func TestAuditClassification(t *testing.T) {
	h := time.Hour
	rep := auditOf(t, []AskRecord{
		ask("http-write", "curl -X POST https://example.com", OutcomeApprove, h),
		ask("http-write", "curl -X POST https://example.com", OutcomeReject, 2*h),
		ask("aws", "aws s3 rm s3://b/k", OutcomeApprove, h),
		ask("aws", "aws s3 rm s3://b/k", OutcomeExpired, 2*h),
		ask("shell-line", "bash -c 'x'", OutcomeApprove, h),
		ask("shell-line", "bash -c 'x'", OutcomeDayAllow, 2*h),
		ask("wrangler", "wrangler deploy", OutcomeApprove, h),
		ask("rm-paths", "rm -rf a", OutcomeApprove, 1*h),
		ask("rm-paths", "rm -rf a", OutcomeApprove, 2*h),
		ask("rm-paths", "rm -rf a", OutcomeApprove, 3*h),
		ask("rm-paths", "rm -rf b", OutcomeApprove, 50*h),
		ask("danger-word", "echo deploy", OutcomeApprove, h),
		ask("danger-word", "echo deploy", OutcomeNoServer, 30*h),
	})
	for rule, want := range map[string][2]string{
		"http-write":  {"curl", ClassValidReject},
		"aws":         {"aws s3", ClassAbsent},
		"shell-line":  {"bash", ClassSkippable},
		"wrangler":    {"wrangler deploy", ClassValidOutward},
		"rm-paths":    {"rm", ClassTedious},
		"danger-word": {"echo", ClassAsIs},
	} {
		if got := findBundle(t, rep, rule, want[0]).Class; got != want[1] {
			t.Errorf("%s: class=%q want %q", rule, got, want[1])
		}
	}
	if rep.Total != 13 || rep.Outcomes[OutcomeApprove] != 9 {
		t.Fatalf("total=%d outcomes=%v", rep.Total, rep.Outcomes)
	}
	if b := findBundle(t, rep, "rm-paths", "rm"); b.Kind != KindDelete || b.Count != 4 {
		t.Fatalf("rm bundle: %+v", b)
	}
	for _, kind := range [][2]string{{"http-write", KindOutward}, {"shell-script", KindUnjudged}, {"unresolved-var", KindUnjudged}, {"guard-boundary", KindBoundary}, {"ownership", KindBoundary}, {"whatever", KindOther}} {
		if RuleKind(kind[0]) != kind[1] {
			t.Errorf("RuleKind(%s)=%s", kind[0], RuleKind(kind[0]))
		}
	}
}

func TestAuditBundlingNamesAndOrder(t *testing.T) {
	h := time.Hour
	other := func(r *AskRecord) { r.Repo = "/work/other" }
	norepo := func(r *AskRecord) { r.Repo = "" }
	rep := auditOf(t, []AskRecord{
		ask("gh-write", "gh pr merge 1", OutcomeApprove, h),
		ask("gh-write", "gh pr merge 2", OutcomeApprove, 2*h),
		ask("gh-write", "gh issue close 3", OutcomeApprove, h),
		ask("git-main", "git -C /x push origin main", OutcomeApprove, h, other),
		ask("git-main", "FOO=1 git push", OutcomeApprove, h, other),
		ask("git-main", "/usr/bin/git push", OutcomeApprove, h, norepo),
		ask("aws", "aws s3 rm s3://x", OutcomeApprove, 8*24*h), // 期間外
	})
	if rep.Total != 6 || rep.BundleCount != 4 {
		t.Fatalf("total=%d bundles=%d", rep.Total, rep.BundleCount)
	}
	if rep.Top[0].Name() != "gh-write・gh pr・app" || rep.Top[0].Count != 2 {
		t.Fatalf("top0=%+v", rep.Top[0])
	}
	if b := findBundle(t, rep, "git-main", "git push"); b.Repo != "other" || b.Count != 2 {
		t.Fatalf("git push other: %+v", b)
	}
	got := map[string]bool{}
	for _, b := range rep.Top {
		got[b.Name()] = true
	}
	if !got["git-main・git push・-"] || len(rep.Top[0].Examples) != 2 {
		t.Fatalf("names=%v examples=%v", got, rep.Top[0].Examples)
	}
	// --top で切る
	small, _ := BuildAudit(AuditOptions{Now: auditNow, Top: 2, Asks: []AskRecord{ask("a", "x", OutcomeApprove, h), ask("b", "y", OutcomeApprove, h), ask("c", "z", OutcomeApprove, h)}})
	if len(small.Top) != 2 || small.BundleCount != 3 {
		t.Fatalf("top=%d bundles=%d", len(small.Top), small.BundleCount)
	}
}

// サーバーの decision と asks.jsonl は decision_id で重複を除く。サーバーに繋がらない時の注も出る。
func TestAuditMergesDecisionsAndServerNote(t *testing.T) {
	created := auditNow.Add(-time.Hour)
	opt := "approve"
	body := func(id, status, title string) api.Decision {
		d := api.Decision{DecisionID: id, Kind: "command_guard", Title: title, Status: status, CreatedUnixMS: created.UnixMilli(),
			Origin: &api.DecisionOrigin{Command: "gh pr merge 1", Cwd: "/work/app"}}
		if status == "answered" {
			d.Answer = &api.DecisionAnswer{OptionID: &opt, AnsweredUnixMS: created.UnixMilli() + 7000}
		}
		return d
	}
	other := body("d9", "answered", "x")
	other.Kind = "ask"
	pending := body("d8", "pending", "実行確認: gh — y（規則: gh-write）")
	rep, err := BuildAudit(AuditOptions{Now: auditNow, Top: 5, ServerNote: "サーバーに繋がらないため…",
		Asks: []AskRecord{ask("gh-write", "gh pr merge 1", OutcomeApprove, time.Hour, func(r *AskRecord) { r.DecisionID, r.Repo = "d1", "" })},
		Decisions: []api.Decision{
			body("d1", "answered", "実行確認: gh — y（規則: gh-write）"), // 重複
			body("d2", "answered", "実行確認: gh — y（規則: gh-write）"),
			body("d3", "expired", "実行確認: gh — y（規則: gh-write）"),
			other, pending,
		}})
	if err != nil {
		t.Fatal(err)
	}
	b := findBundle(t, rep, "gh-write", "gh pr")
	if rep.Total != 3 || b.Count != 3 || b.Outcomes[OutcomeExpired] != 1 || b.Class != ClassAbsent {
		t.Fatalf("total=%d bundle=%+v", rep.Total, b)
	}
	if b.WaitMedianMS != 7000 && b.WaitMedianMS != 3000 {
		t.Fatalf("median=%d", b.WaitMedianMS)
	}
	var text bytes.Buffer
	RenderAuditText(&text, rep)
	if !strings.Contains(text.String(), "サーバーに繋がらないため") || !strings.Contains(text.String(), "gh-write・gh pr・-") {
		t.Fatalf("text=%s", text.String())
	}
	// origin.guard_rule があればタイトルより優先する
	d := body("d4", "answered", "実行確認: gh — y（規則: gh-write）")
	d.Origin.GuardRule = "aws"
	if r, ok := DecisionAsk(d); !ok || r.Rule != "aws" {
		t.Fatalf("rule=%+v", r)
	}
}

// replay: 却下された件が今の規則で allow になると「要注意」に出る。
func TestAuditReplayAttention(t *testing.T) {
	dir := t.TempDir()
	h := time.Hour
	at := func(r *AskRecord) { r.Cwd, r.Repo = dir, "" }
	rep, err := BuildAudit(AuditOptions{Now: auditNow, Top: 5, Replay: true, Config: Config{}, Env: map[string]string{"HOME": dir, "PATH": "/usr/bin:/bin"}, Asks: []AskRecord{
		ask("danger-word", "echo hello", OutcomeReject, h, at),
		ask("danger-word", "echo hello", OutcomeApprove, 2*h, at),
		ask("danger-word", "echo hello", OutcomeApprove, 3*h, func(r *AskRecord) { r.Cwd = "" }), // cwd が無い件は対象外
		ask("wrangler", "npx wrangler deploy --env staging", OutcomeReject, h, at),
	}})
	if err != nil {
		t.Fatal(err)
	}
	e := findBundle(t, rep, "danger-word", "echo")
	if e.ReplayChecked != 2 || e.ReplayAllow != 2 {
		t.Fatalf("echo replay: %+v", e)
	}
	w := findBundle(t, rep, "wrangler", "npx")
	if w.ReplayChecked != 1 || w.ReplayAllow != 0 {
		t.Fatalf("wrangler replay: %+v", w)
	}
	if len(rep.Attention) != 1 || rep.Attention[0].Command != "echo hello" {
		t.Fatalf("attention=%+v", rep.Attention)
	}
	var text bytes.Buffer
	RenderAuditText(&text, rep)
	if !strings.Contains(text.String(), "要注意") || strings.Index(text.String(), "要注意") > strings.Index(text.String(), "上位の束") {
		t.Fatalf("attention must come first:\n%s", text.String())
	}
}

func writeDecisionLog(t *testing.T, path string, n int, epochOf func(int) int64, missed func(int) bool) {
	t.Helper()
	var b bytes.Buffer
	for k := 0; k < n; k++ {
		rec := Record{Epoch: epochOf(k), Timestamp: time.Unix(epochOf(k), 0).UTC().Format(time.RFC3339), Decision: "allow", Rule: "shell-line", Command: []string{"x"}, CommandLine: fmt.Sprintf("echo line-%d padding %s", k, strings.Repeat("p", 200)), Cwd: "/w", Via: "line"}
		if missed(k) {
			rec.Reason = undeterminedPrefix + "変数の中身が分からない"
			rec.CommandLine = fmt.Sprintf("curl -X POST https://example.com/%d token=fixture-secret", k)
		}
		line, _ := json.Marshal(rec)
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 後ろから読み、期間より古い行で止まる。ファイル全体は読まない。
func TestSampleMissedStopsAtSinceAndReadsFromTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	const total, recent = 20000, 2000 // 約 6MB。後ろ 2000 行が期間内
	base := auditNow.Unix() - 100000
	writeDecisionLog(t, path, total, func(k int) int64 { return base + int64(k) }, func(k int) bool { return k%7 == 0 })
	st, _ := os.Stat(path)
	sinceEpoch := base + int64(total-recent)
	samples, read, err := SampleMissed(path, sinceEpoch, 30, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 30 {
		t.Fatalf("samples=%d", len(samples))
	}
	if read >= st.Size()/2 || read%tailChunk != 0 && read != st.Size() {
		t.Fatalf("read=%d of %d: the whole file was read", read, st.Size())
	}
	if read > 2*tailChunk {
		t.Fatalf("read=%d: more than needed for %d recent lines", read, recent)
	}
	for _, s := range samples {
		if strings.Contains(s.CmdLine, "fixture-secret") || len(s.CmdLine) > 200 {
			t.Fatalf("not redacted/clipped: %q", s.CmdLine)
		}
		if ts, _ := time.Parse(time.RFC3339, s.TS); ts.Unix() < sinceEpoch {
			t.Fatalf("old sample %s", s.TS)
		}
	}
	// 期間内の該当が30件に満たなければ全部返す。全部の期間なら先頭まで読む。
	few, _, _ := SampleMissed(path, base+int64(total-20), 30, rand.New(rand.NewSource(1)))
	if len(few) == 0 || len(few) > 3 {
		t.Fatalf("few=%d", len(few))
	}
	_, all, _ := SampleMissed(path, 0, 30, rand.New(rand.NewSource(1)))
	if all != st.Size() {
		t.Fatalf("full read=%d size=%d", all, st.Size())
	}
	if none, n, err := SampleMissed(filepath.Join(t.TempDir(), "none"), 0, 30, nil); err != nil || n != 0 || none != nil {
		t.Fatalf("missing file: %v %d %v", none, n, err)
	}
}

func TestSampleMissedFiltersByVerdictReasonAndWord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	rows := []Record{
		{Epoch: auditNow.Unix(), Decision: "allow", Reason: undeterminedPrefix + "x", CommandLine: "ls -la"},                    // 外向きの語なし
		{Epoch: auditNow.Unix(), Decision: "ask", Reason: undeterminedPrefix + "x", CommandLine: "curl https://e.com"},          // allow ではない
		{Epoch: auditNow.Unix(), Decision: "allow", Reason: "別の理由", CommandLine: "curl https://e.com"},                          // 理由が違う
		{Epoch: auditNow.Unix(), Decision: "allow", Reason: undeterminedPrefix + "x", CommandLine: "echo high"},                 // gh を含む語だけ
		{Epoch: auditNow.Unix(), Decision: "allow", Reason: undeterminedPrefix + "x", CommandLine: "cd a && git push origin x"}, // 該当
		{Epoch: auditNow.Unix(), Decision: "allow", Reason: undeterminedPrefix + "x", CommandLine: "npm publish"},               // 該当
	}
	var b bytes.Buffer
	for _, r := range rows {
		line, _ := json.Marshal(r)
		b.Write(line)
		b.WriteByte('\n')
	}
	os.WriteFile(path, b.Bytes(), 0o600)
	s, _, err := SampleMissed(path, 0, 30, rand.New(rand.NewSource(1)))
	if err != nil || len(s) != 2 {
		t.Fatalf("%+v %v", s, err)
	}
	rep, _ := BuildAudit(AuditOptions{Now: auditNow, SampleLog: path, Since: 365 * 24 * time.Hour, Rand: rand.New(rand.NewSource(1))})
	html := RenderAuditHTML(rep)
	if len(rep.Missed) != 2 || !strings.Contains(html, "見逃し側の見本") || !strings.Contains(html, "npm publish") || strings.Contains(html, "http://") {
		t.Fatalf("html/missed: %d", len(rep.Missed))
	}
}

func TestAuditHTMLIsSelfContainedAndEscaped(t *testing.T) {
	rep := auditOf(t, []AskRecord{ask("http-write", "curl <script>alert(1)</script>", OutcomeApprove, time.Hour)})
	h := RenderAuditHTML(rep)
	for _, bad := range []string{"<script", "src=", "href=", "mermaid", "@import", "url("} {
		if strings.Contains(h, bad) {
			t.Errorf("html contains %q", bad)
		}
	}
	if !strings.Contains(h, "&lt;script&gt;") || !strings.Contains(h, "http-write・curl・app") || !strings.Contains(h, "全体") {
		t.Fatalf("html=%s", h)
	}
	var j bytes.Buffer
	if err := RenderAuditJSON(&j, rep); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if json.Unmarshal(j.Bytes(), &back) != nil || back["total"].(float64) != 1 || back["missed_samples"] == nil {
		t.Fatalf("json=%s", j.String())
	}
}

func TestParseSinceAndCommandName(t *testing.T) {
	for in, want := range map[string]string{"": "-", "ls -la": "ls", "/usr/bin/curl x": "curl", "git -c a=b -C /x push": "git push", "gh -v pr merge": "gh pr", "AI_GUARD_REQUEST_REASON=x npm publish": "npm publish", "git": "git"} {
		if got := CommandName(in); got != want {
			t.Errorf("CommandName(%q)=%q want %q", in, got, want)
		}
	}
}
