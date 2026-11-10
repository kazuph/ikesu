package guard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// 規則の種類。規則名は1か所のこの表で決める。
const (
	KindOutward  = "外向き"
	KindDelete   = "削除"
	KindUnjudged = "判定不能"
	KindBoundary = "guard 境界"
	KindOther    = "その他"
)

var auditRuleKinds = map[string]string{
	"http-write": KindOutward, "sql-write": KindOutward, "aws": KindOutward, "wrangler": KindOutward,
	"gh-write": KindOutward, "git-main": KindOutward, "git-push-force": KindOutward,
	"danger-word": KindOutward, "danger-word-line": KindOutward,
	"rm-paths":   KindDelete,
	"shell-line": KindUnjudged, "shell-script": KindUnjudged,
	"guard-boundary": KindBoundary, "ownership": KindBoundary,
}

// RuleKind は規則名の種類。unresolved 系は判定不能。
func RuleKind(rule string) string {
	if k, ok := auditRuleKinds[rule]; ok {
		return k
	}
	if strings.HasPrefix(rule, "unresolved") {
		return KindUnjudged
	}
	return KindOther
}

// 分類。
const (
	ClassValidReject  = "妥当（却下あり）"
	ClassAbsent       = "不在で止まった"
	ClassSkippable    = "聞かなくてよい候補"
	ClassValidOutward = "妥当（外向き）"
	ClassTedious      = "手間"
	ClassAsIs         = "そのまま"
)

// Bundle は 規則 × 命令名 × リポジトリ の束。
type Bundle struct {
	Rule          string         `json:"rule"`
	Kind          string         `json:"kind"`
	Command       string         `json:"command"`
	Repo          string         `json:"repo"`
	Count         int            `json:"count"`
	Outcomes      map[string]int `json:"outcomes"`
	WaitMedianMS  int64          `json:"wait_median_ms"`
	Examples      []string       `json:"examples"`
	First         string         `json:"first"`
	Last          string         `json:"last"`
	Class         string         `json:"class"`
	ReplayChecked int            `json:"replay_checked,omitempty"`
	ReplayAllow   int            `json:"replay_now_allow,omitempty"`
	records       []AskRecord
}

// Name は束の短い名前。
func (b Bundle) Name() string { return b.Rule + "・" + b.Command + "・" + b.Repo }

// Attention は却下されたのに今の規則なら allow になる件。
type Attention struct {
	TS      string `json:"ts"`
	Rule    string `json:"rule"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Bundle  string `json:"bundle"`
}

type MissedSample struct {
	TS      string `json:"ts"`
	Rule    string `json:"rule"`
	CmdLine string `json:"cmd_line"`
	Cwd     string `json:"cwd,omitempty"`
	PaneID  string `json:"pane_id,omitempty"`
}

type AuditReport struct {
	From        string         `json:"from"`
	To          string         `json:"to"`
	Total       int            `json:"total"`
	Outcomes    map[string]int `json:"outcomes"`
	BundleCount int            `json:"bundle_count"`
	Top         []Bundle       `json:"top"`
	Replay      bool           `json:"replay"`
	Attention   []Attention    `json:"attention,omitempty"`
	Missed      []MissedSample `json:"missed_samples"`
	Notes       []string       `json:"notes,omitempty"`
}

type AuditOptions struct {
	Root       string
	Now        time.Time
	Since      time.Duration
	Top        int
	Asks       []AskRecord    // asks.jsonl から
	Decisions  []api.Decision // サーバーの decision.list から
	ServerNote string         // サーバーに繋がらなかった時の説明
	Replay     bool
	Config     Config
	SampleLog  string // decisions.jsonl（空なら見逃し側の見本を作らない）
	Rand       *rand.Rand
	Env        map[string]string
}

// ReadAsks は asks.jsonl を読む。ファイルが無ければ空。
func ReadAsks(path string, sinceEpoch int64) ([]AskRecord, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []AskRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var r AskRecord
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.TSEpoch < sinceEpoch {
			continue
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

var titleRule = regexp.MustCompile(`（規則: ([^）]*)）\s*$`)

// DecisionAsk は サーバーの回答済み decision を AskRecord にする。command_guard 以外・回答前のものは対象外。
func DecisionAsk(d api.Decision) (AskRecord, bool) {
	if d.Kind != "command_guard" {
		return AskRecord{}, false
	}
	r := AskRecord{DecisionID: d.DecisionID, TSEpoch: d.CreatedUnixMS / 1000}
	r.TS = time.Unix(r.TSEpoch, 0).UTC().Format(time.RFC3339)
	var end int64
	switch d.Status {
	case "expired":
		r.Outcome = OutcomeExpired
	case "cancelled":
		r.Outcome = OutcomeCancelled
	case "answered":
		choice, text := "", ""
		if d.Answer != nil {
			if d.Answer.OptionID != nil {
				choice = *d.Answer.OptionID
			}
			if d.Answer.Text != nil {
				text = strings.TrimSpace(*d.Answer.Text)
			}
			end = d.Answer.AnsweredUnixMS
		}
		switch {
		case choice == "approve":
			r.Outcome = OutcomeApprove
		case choice == "day_allow":
			r.Outcome = OutcomeDayAllow
		case choice == "" && text == "":
			r.Outcome = OutcomeCancelled
		default:
			r.Outcome = OutcomeReject
		}
		r.AnswerText = truncateRunes(redact(text), answerTextLimit)
	default:
		return AskRecord{}, false
	}
	if end > d.CreatedUnixMS {
		r.WaitMS = end - d.CreatedUnixMS
	}
	if o := d.Origin; o != nil {
		r.Rule, r.Reason = o.GuardRule, o.GuardReason
		r.Command, r.Cwd, r.PaneID, r.Agent = o.Command, o.Cwd, o.PaneID, o.Agent
		r.Units = askUnits(o.CommandUnits)
	}
	if r.Rule == "" {
		if m := titleRule.FindStringSubmatch(d.Title); m != nil {
			r.Rule = m[1]
		}
	}
	r.Repo = repoOf(r.Cwd)
	return r, true
}

var subcommandTools = map[string]bool{"git": true, "gh": true, "npm": true, "pnpm": true, "yarn": true, "bun": true, "npx": false,
	"aws": true, "wrangler": true, "docker": true, "cargo": true, "ikesu": true, "brew": true, "kubectl": true, "terraform": true, "go": true}

// CommandName は command の最初の語の basename。git・gh・npm などは下位コマンドまで。
func CommandName(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 {
		f := fields[0]
		if eq := strings.Index(f, "="); eq > 0 && !strings.ContainsAny(f[:eq], "/$\"'") {
			fields = fields[1:]
			continue
		}
		break
	}
	if len(fields) == 0 {
		return "-"
	}
	name := filepath.Base(fields[0])
	if !subcommandTools[name] {
		return name
	}
	rest := fields[1:]
	for n := 0; n < len(rest); n++ {
		t := rest[n]
		if (t == "-C" || t == "-c") && name == "git" {
			n++
			continue
		}
		if strings.HasPrefix(t, "-") {
			continue
		}
		return name + " " + t
	}
	return name
}

func repoName(repo string) string {
	if repo == "" {
		return "-"
	}
	return filepath.Base(repo)
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(a, b int) bool { return v[a] < v[b] })
	return v[len(v)/2]
}

func classifyBundle(b *Bundle, loc *time.Location) string {
	o := b.Outcomes
	if o[OutcomeReject] > 0 {
		return ClassValidReject
	}
	if o[OutcomeExpired] > 0 {
		return ClassAbsent
	}
	allowed := o[OutcomeApprove]+o[OutcomeDayAllow] == b.Count
	if (b.Kind == KindUnjudged || b.Kind == KindBoundary) && allowed {
		return ClassSkippable
	}
	if b.Kind == KindOutward && allowed {
		return ClassValidOutward
	}
	perDay := map[string]int{}
	for _, r := range b.records {
		day := time.Unix(r.TSEpoch, 0).In(loc).Format("2006-01-02")
		perDay[day]++
		if perDay[day] >= 3 {
			return ClassTedious
		}
	}
	return ClassAsIs
}

// BuildAudit は材料を束ね、分類し、必要なら今の規則で判定し直し、見逃し側の見本を集める。
func BuildAudit(o AuditOptions) (*AuditReport, error) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Since <= 0 {
		o.Since = 7 * 24 * time.Hour
	}
	if o.Top <= 0 {
		o.Top = 5
	}
	sinceEpoch := o.Now.Add(-o.Since).Unix()
	rep := &AuditReport{From: o.Now.Add(-o.Since).Format(time.RFC3339), To: o.Now.Format(time.RFC3339), Outcomes: map[string]int{}, Replay: o.Replay}
	if o.ServerNote != "" {
		rep.Notes = append(rep.Notes, o.ServerNote)
	}
	seen := map[string]bool{}
	var all []AskRecord
	for _, r := range o.Asks {
		if r.TSEpoch < sinceEpoch {
			continue
		}
		if r.DecisionID != "" {
			seen[r.DecisionID] = true
		}
		all = append(all, r)
	}
	for _, d := range o.Decisions {
		r, ok := DecisionAsk(d)
		if !ok || r.TSEpoch < sinceEpoch || seen[r.DecisionID] {
			continue
		}
		seen[r.DecisionID] = true
		all = append(all, r)
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].TSEpoch < all[b].TSEpoch })
	bundles := map[string]*Bundle{}
	var order []string
	for _, r := range all {
		rep.Total++
		rep.Outcomes[r.Outcome]++
		name := CommandName(r.Command)
		repo := repoName(r.Repo)
		key := r.Rule + "\x00" + name + "\x00" + repo
		b := bundles[key]
		if b == nil {
			b = &Bundle{Rule: r.Rule, Kind: RuleKind(r.Rule), Command: name, Repo: repo, Outcomes: map[string]int{}, First: r.TS}
			bundles[key] = b
			order = append(order, key)
		}
		b.Count++
		b.Outcomes[r.Outcome]++
		b.Last = r.TS
		b.records = append(b.records, r)
	}
	list := make([]*Bundle, 0, len(order))
	for _, k := range order {
		list = append(list, bundles[k])
	}
	for _, b := range list {
		var waits []int64
		for _, r := range b.records {
			if r.WaitMS > 0 {
				waits = append(waits, r.WaitMS)
			}
		}
		b.WaitMedianMS = median(waits)
		for n := len(b.records) - 1; n >= 0 && len(b.Examples) < 2; n-- {
			b.Examples = append(b.Examples, truncateRunes(b.records[n].Command, 200))
		}
		b.Class = classifyBundle(b, o.Now.Location())
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Count > list[b].Count })
	rep.BundleCount = len(list)
	if len(list) > o.Top {
		list = list[:o.Top]
	}
	if o.Replay {
		replayBundles(rep, list, o)
	}
	for _, b := range list {
		rep.Top = append(rep.Top, *b)
	}
	if o.SampleLog != "" {
		rng := o.Rand
		if rng == nil {
			rng = rand.New(rand.NewSource(o.Now.UnixNano()))
		}
		s, _, err := SampleMissed(o.SampleLog, sinceEpoch, 30, rng)
		if err != nil {
			rep.Notes = append(rep.Notes, "見逃し側の見本を読めませんでした: "+err.Error())
		}
		rep.Missed = s
	}
	if rep.Missed == nil {
		rep.Missed = []MissedSample{}
	}
	return rep, nil
}

const replayPerBundle = 100

func replayBundles(rep *AuditReport, list []*Bundle, o AuditOptions) {
	env := map[string]string{}
	for k, v := range o.Env {
		env[k] = v
	}
	if env["HOME"] == "" {
		env["HOME"] = os.Getenv("HOME")
	}
	// 今日の許可記録で allow にならないよう、存在しない記録ファイルを指す。
	env["AI_GUARD_TEMP_APPROVAL_FILE"] = filepath.Join(os.TempDir(), "ikesu-audit-replay-none")
	for _, b := range list {
		recs := b.records
		if len(recs) > replayPerBundle {
			recs = recs[len(recs)-replayPerBundle:]
		}
		for _, r := range recs {
			if r.Command == "" || r.Cwd == "" {
				continue
			}
			_, v, err := lineVerdict(o.Config, Input{Cwd: r.Cwd, LaunchCwd: r.Cwd, Env: env}, r.Command)
			if err != nil {
				continue
			}
			b.ReplayChecked++
			if v.Decision != "allow" {
				continue
			}
			b.ReplayAllow++
			if r.Outcome == OutcomeReject {
				rep.Attention = append(rep.Attention, Attention{TS: r.TS, Rule: r.Rule, Command: truncateRunes(r.Command, 200), Cwd: r.Cwd, Bundle: b.Name()})
			}
		}
	}
}

var outwardWord = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])(?:curl|wget|gh|wrangler|aws|psql|mysql|sqlite3|ssh|scp|rsync|git\s+push|npm\s+publish|deploy)(?:[^A-Za-z0-9_-]|$)`)

type missedLine struct {
	Epoch   int64  `json:"ts_epoch"`
	TS      string `json:"ts"`
	Verdict string `json:"verdict"`
	Rule    string `json:"rule"`
	Reason  string `json:"reason"`
	CmdLine string `json:"cmd_line"`
	Cwd     string `json:"cwd"`
	Pane    string `json:"pane_id"`
}

const tailChunk = 1 << 20

// SampleMissed は decisions.jsonl を後ろから 1MB ずつ読み、sinceEpoch より古い行で止まる。
// 「未確定を通した: 」で allow になり外向きの語を含む行から n 件を無作為に選ぶ。
func SampleMissed(path string, sinceEpoch int64, n int, rng *rand.Rand) ([]MissedSample, int64, error) {
	return sampleMissed(path, sinceEpoch, n, rng)
}

func sampleMissed(path string, sinceEpoch int64, n int, rng *rand.Rand) ([]MissedSample, int64, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	pos := st.Size()
	var carry []byte
	var read int64
	var picked []MissedSample
	seen := 0
	done := false
	handle := func(line []byte) {
		if len(bytes.TrimSpace(line)) == 0 {
			return
		}
		// 多くの行は「未確定を通した」を含まない。その行は時刻だけを安く読んで、期間の終わりかを見る。
		if !bytes.Contains(line, undeterminedPrefixBytes) {
			if e, ok := quickEpoch(line); ok && e < sinceEpoch {
				done = true
			}
			return
		}
		var m missedLine
		if json.Unmarshal(line, &m) != nil {
			return
		}
		if m.Epoch < sinceEpoch {
			done = true
			return
		}
		if m.Verdict != "allow" || !strings.HasPrefix(m.Reason, undeterminedPrefix) || !outwardWord.MatchString(m.CmdLine) {
			return
		}
		s := MissedSample{TS: m.TS, Rule: m.Rule, CmdLine: truncateRunes(redact(m.CmdLine), 200), Cwd: m.Cwd, PaneID: m.Pane}
		seen++
		if len(picked) < n {
			picked = append(picked, s)
		} else if j := rng.Intn(seen); j < n {
			picked[j] = s
		}
	}
	for pos > 0 && !done && read < maxMissedScanBytes {
		size := int64(tailChunk)
		if pos < size {
			size = pos
		}
		pos -= size
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return picked, read, err
		}
		read += size
		data := append(buf, carry...)
		lines := bytes.Split(data, []byte{'\n'})
		start := 0
		if pos > 0 {
			carry = lines[0]
			start = 1
		} else {
			carry = nil
		}
		for k := len(lines) - 1; k >= start && !done; k-- {
			handle(lines[k])
		}
	}
	sort.Slice(picked, func(a, b int) bool { return picked[a].TS < picked[b].TS })
	return picked, read, nil
}

func fmtWait(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1f秒", float64(ms)/1000)
	}
	return fmt.Sprintf("%.1f分", float64(ms)/60000)
}

var outcomeOrder = []string{OutcomeApprove, OutcomeDayAllow, OutcomeReject, OutcomeExpired, OutcomeCancelled, OutcomeNoReason, OutcomeNoServer}

func outcomeText(m map[string]int) string {
	var parts []string
	for _, k := range outcomeOrder {
		if m[k] > 0 {
			parts = append(parts, k+" "+strconv.Itoa(m[k]))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " / ")
}

// RenderAuditText は文字の出力。
func RenderAuditText(w io.Writer, r *AuditReport) {
	fmt.Fprintf(w, "期間: %s 〜 %s\n", r.From, r.To)
	fmt.Fprintf(w, "全体: %d件（%s） 束の数: %d\n", r.Total, outcomeText(r.Outcomes), r.BundleCount)
	for _, n := range r.Notes {
		fmt.Fprintf(w, "注: %s\n", n)
	}
	if len(r.Attention) > 0 {
		fmt.Fprintln(w, "\n要注意（却下されたが、今の規則なら聞かない）:")
		for _, a := range r.Attention {
			fmt.Fprintf(w, "  %s [%s] %s (%s)\n", localTime(a.TS), a.Bundle, a.Command, a.Cwd)
		}
	}
	fmt.Fprintf(w, "\n上位の束（%d件）:\n", len(r.Top))
	for n, b := range r.Top {
		fmt.Fprintf(w, "%d. %s  [%s] %d件  結果: %s  待ち中央値: %s  分類: %s\n", n+1, b.Name(), b.Kind, b.Count, outcomeText(b.Outcomes), fmtWait(b.WaitMedianMS), b.Class)
		for _, e := range b.Examples {
			fmt.Fprintf(w, "     例: %s\n", e)
		}
		if r.Replay {
			fmt.Fprintf(w, "     今の規則なら聞かない: %d/%d件\n", b.ReplayAllow, b.ReplayChecked)
		}
	}
	if len(r.Missed) > 0 {
		fmt.Fprintf(w, "\n見逃し側の見本（未確定を通した外向きの行、%d件）:\n", len(r.Missed))
		for _, m := range r.Missed {
			fmt.Fprintf(w, "  %s %s\n", localTime(m.TS), m.CmdLine)
		}
	}
}

func RenderAuditJSON(w io.Writer, r *AuditReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderAuditHTML は外部ファイルを読まない単一の HTML。
func RenderAuditHTML(r *AuditReport) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>コマンド承認の見直し</title><style>
:root{color-scheme:light;--bg:#f6f7f9;--card:#fff;--ink:#1d2430;--mute:#667085;--line:#dfe3ea;--accent:#2457c5;--warn:#b42318;--ok:#16794c}
body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.6 -apple-system,"Hiragino Sans",sans-serif}
main{max-width:960px;margin:0 auto;padding:24px 16px}
h1{font-size:22px;margin:0 0 4px}h2{font-size:17px;margin:28px 0 10px}
.sum{display:flex;flex-wrap:wrap;gap:8px;margin:12px 0}.sum span{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:6px 12px}
.card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin:12px 0}
.card h3{margin:0 0 6px;font-size:15px}.tag{display:inline-block;border-radius:999px;padding:1px 10px;font-size:12px;background:#e8eefc;color:var(--accent);margin-right:6px}
.tag.cls{background:#eef6f0;color:var(--ok)}.tag.bad{background:#fdecea;color:var(--warn)}
code,pre{font:12px/1.5 ui-monospace,Menlo,monospace}pre{background:#f1f3f7;border-radius:6px;padding:8px 10px;margin:6px 0;white-space:pre-wrap;word-break:break-all}
.mute{color:var(--mute)}.warn{color:var(--warn)}
</style></head><body><main>`)
	b.WriteString("<h1>コマンド承認の見直し</h1>")
	fmt.Fprintf(&b, `<div class="mute">期間: %s 〜 %s</div>`, e(r.From), e(r.To))
	fmt.Fprintf(&b, `<div class="sum"><span>全体 <b>%d</b>件</span><span>束 <b>%d</b>つ</span><span>%s</span></div>`, r.Total, r.BundleCount, e(outcomeText(r.Outcomes)))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, `<p class="mute">注: %s</p>`, e(n))
	}
	if len(r.Attention) > 0 {
		b.WriteString(`<h2 class="warn">要注意（却下されたが、今の規則なら聞かない）</h2>`)
		for _, a := range r.Attention {
			fmt.Fprintf(&b, `<div class="card"><span class="tag bad">要注意</span>%s<pre>%s</pre><div class="mute">%s / %s</div></div>`, e(a.Bundle), e(a.Command), e(localTime(a.TS)), e(a.Cwd))
		}
	}
	fmt.Fprintf(&b, `<h2>上位の束（%d件）</h2>`, len(r.Top))
	for n, x := range r.Top {
		fmt.Fprintf(&b, `<div class="card"><h3>%d. %s</h3><span class="tag">%s</span><span class="tag cls">%s</span> <b>%d</b>件`, n+1, e(x.Name()), e(x.Kind), e(x.Class), x.Count)
		fmt.Fprintf(&b, `<div>結果: %s ／ 待ち時間の中央値: %s</div><div class="mute">初回 %s ／ 最後 %s</div>`, e(outcomeText(x.Outcomes)), e(fmtWait(x.WaitMedianMS)), e(localTime(x.First)), e(localTime(x.Last)))
		if r.Replay {
			fmt.Fprintf(&b, `<div>今の規則なら聞かない: %d / %d件</div>`, x.ReplayAllow, x.ReplayChecked)
		}
		for _, ex := range x.Examples {
			fmt.Fprintf(&b, `<pre>%s</pre>`, e(ex))
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<h2>見逃し側の見本</h2><p class="mute">判定できずに通した行のうち、外向きの語を含むものから無作為に選んだ見本です。規則に漏れがないか見る材料です。</p>`)
	if len(r.Missed) == 0 {
		b.WriteString(`<p class="mute">該当なし</p>`)
	}
	for _, m := range r.Missed {
		fmt.Fprintf(&b, `<div class="card"><div class="mute">%s %s</div><pre>%s</pre></div>`, e(localTime(m.TS)), e(m.Cwd), e(m.CmdLine))
	}
	b.WriteString(`</main></body></html>`)
	return b.String()
}

// localTime は記録の時刻（RFC3339）を手元の時刻で短く見せる。読めなければそのまま返す。
func localTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.In(time.Local).Format("2006-01-02 15:04")
}

// maxMissedScanBytes は見逃し側の見本のために decisions.jsonl を後ろから読む上限。期間が長くても読む量を抑える。
const maxMissedScanBytes = 256 << 20

var undeterminedPrefixBytes = []byte(undeterminedPrefix)

// quickEpoch は記録の1行から "ts_epoch" の数字だけを読む。
func quickEpoch(line []byte) (int64, bool) {
	k := bytes.Index(line, []byte(`"ts_epoch":`))
	if k < 0 {
		return 0, false
	}
	rest := line[k+len(`"ts_epoch":`):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(string(rest[:end]), 10, 64)
	return v, err == nil
}
