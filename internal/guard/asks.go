package guard

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
)

// AskRecord は「聞かれた1件とその結果」。<root>/guard/asks.jsonl に1行ずつ追記し、
// 見直し（ikesu guard audit）の材料にする。
type AskRecord struct {
	TS         string    `json:"ts"`
	TSEpoch    int64     `json:"ts_epoch"`
	DecisionID string    `json:"decision_id,omitempty"`
	Rule       string    `json:"rule"`
	Reason     string    `json:"reason,omitempty"`
	Command    string    `json:"command"`
	Units      []AskUnit `json:"units,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	Repo       string    `json:"repo,omitempty"`
	PaneID     string    `json:"pane_id,omitempty"`
	Workspace  string    `json:"workspace_id,omitempty"` // 分析用。許可の判断には使わない
	Agent      string    `json:"agent,omitempty"`
	Via        string    `json:"via,omitempty"`
	Outcome    string    `json:"outcome"`
	WaitMS     int64     `json:"wait_ms"`
	AnswerText string    `json:"answer_text,omitempty"`
	Tier       int       `json:"tier,omitempty"`
	Dest       string    `json:"dest,omitempty"`
	TierRule   string    `json:"tier_rule,omitempty"`
	AllowedBy  string    `json:"allowed_by,omitempty"`
}

// AskUnit は command_units のうち ask・block の行だけを残したもの。
type AskUnit struct {
	Label  string `json:"label"`
	Rule   string `json:"rule,omitempty"`
	Target string `json:"target,omitempty"`
}

// 結果の名前。
const (
	OutcomeApprove   = "approve"
	OutcomeDayAllow  = "day_allow"
	OutcomeReject    = "reject"
	OutcomeExpired   = "expired"
	OutcomeCancelled = "cancelled"
	OutcomeNoReason  = "no_reason"
	OutcomeNoServer  = "no_server"
	OutcomeL1Allow   = "l1_allow" // L1 でペインの中だったので、聞かずに通した
	OutcomeOneShot   = "one_shot" // L3 を承認した直後の、別の段での同じコマンドの判定を1回だけ通した
)

const answerTextLimit = 500

func AsksPath(root string) string { return filepath.Join(root, "guard", "asks.jsonl") }

// AppendAsk は asks.jsonl へ1行追記する（0600、排他ロック）。
func AppendAsk(root string, r AskRecord) error {
	d := filepath.Join(root, "guard")
	if e := os.MkdirAll(d, 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(AsksPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		return e
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	return e
}

// repoOf は cwd から上へたどって最初の .git のあるフォルダ。git は呼ばず、ファイルの有無で見る。
func repoOf(cwd string) string {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return ""
	}
	dir := filepath.Clean(cwd)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func askUnits(units []api.CommandUnit) []AskUnit {
	var out []AskUnit
	for _, u := range units {
		if u.Verdict == "ask" || u.Verdict == "block" {
			out = append(out, AskUnit{Label: u.Label, Rule: u.Rule, Target: u.Target})
		}
	}
	return out
}

// recordAsk は判定の結果を asks.jsonl に残す。失敗しても判定は変えず、stderr に1行だけ出す。
func recordAsk(root string, i Input, m Marker, v Verdict, via, command, outcome string, d *api.Decision, started time.Time, errOut io.Writer) {
	now := time.Now()
	r := AskRecord{
		TS: now.UTC().Format(time.RFC3339), TSEpoch: now.Unix(),
		Rule: v.Rule, Reason: i.redact(v.Reason), Command: truncateRunes(i.redact(command), 4000),
		Units: askUnits(i.commandUnits(command)),
		Cwd:   i.redact(i.Cwd), Repo: i.redact(repoOf(i.Cwd)),
		PaneID: m.Pane, Workspace: m.Workspace, Agent: m.Agent, Via: via, Outcome: outcome,
		WaitMS: now.Sub(started).Milliseconds(),
		Tier:   v.Tier, Dest: v.Dest, TierRule: v.TierRule,
	}
	switch outcome {
	case OutcomeL1Allow:
		r.AllowedBy = "l1-default"
	case OutcomeOneShot:
		r.AllowedBy = "one-shot"
	case OutcomeApprove, OutcomeDayAllow:
		r.AllowedBy = "approve"
	}
	if d != nil {
		r.DecisionID = d.DecisionID
		if d.Answer != nil && d.Answer.Text != nil && strings.TrimSpace(*d.Answer.Text) != "" {
			r.AnswerText = truncateRunes(i.redact(*d.Answer.Text), answerTextLimit)
		}
	}
	if e := AppendAsk(root, r); e != nil {
		errOut.Write([]byte("guard asks log: " + e.Error() + "\n"))
	}
}
