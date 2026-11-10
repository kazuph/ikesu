package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

const DefaultAuditWeekly = "mon 10:00"

// WeeklySpec は audit_weekly の中身。
type WeeklySpec struct {
	Off  bool
	Day  time.Weekday
	Hour int
	Min  int
}

var weekdayNames = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}

// ParseWeekly は "mon 10:00"（曜日は mon〜sun、時刻は手元の時刻）か "off" を読む。空は既定値。
func ParseWeekly(spec string) (WeeklySpec, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "" {
		spec = DefaultAuditWeekly
	}
	if spec == "off" {
		return WeeklySpec{Off: true}, nil
	}
	f := strings.Fields(spec)
	if len(f) != 2 {
		return WeeklySpec{}, fmt.Errorf("guard audit_weekly は \"mon 10:00\" か \"off\" の形で書いてください: %q", spec)
	}
	day, ok := weekdayNames[f[0]]
	if !ok {
		return WeeklySpec{}, fmt.Errorf("guard audit_weekly の曜日は mon〜sun のどれかです: %q", f[0])
	}
	hm := strings.Split(f[1], ":")
	if len(hm) != 2 {
		return WeeklySpec{}, fmt.Errorf("guard audit_weekly の時刻は HH:MM の形です: %q", f[1])
	}
	h, e1 := strconv.Atoi(hm[0])
	m, e2 := strconv.Atoi(hm[1])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return WeeklySpec{}, fmt.Errorf("guard audit_weekly の時刻が正しくありません: %q", f[1])
	}
	return WeeklySpec{Day: day, Hour: h, Min: m}, nil
}

// LastSlot は now 以前でいちばん新しい、その曜日・時刻の時点。
func (w WeeklySpec) LastSlot(now time.Time) time.Time {
	slot := time.Date(now.Year(), now.Month(), now.Day(), w.Hour, w.Min, 0, 0, now.Location())
	slot = slot.AddDate(0, 0, -((int(now.Weekday()) - int(w.Day) + 7) % 7))
	if slot.After(now) {
		slot = slot.AddDate(0, 0, -7)
	}
	return slot
}

// Due は今、週1回の見直しを作る番かどうか。lastDay は last-weekly に書いてある日（YYYY-MM-DD、無ければ空）。
// 最後に時刻を過ぎた枠の日より前にしか作っていなければ作る。止まっていた間の取り戻しもこれで足りる。
func (w WeeklySpec) Due(now time.Time, lastDay string) bool {
	if w.Off {
		return false
	}
	if _, err := time.Parse("2006-01-02", lastDay); err != nil {
		return true
	}
	return lastDay < w.LastSlot(now).Format("2006-01-02")
}

func AuditDir(root string) string { return filepath.Join(root, "guard", "audit") }

func ReadLastWeekly(root string) string {
	b, err := os.ReadFile(filepath.Join(AuditDir(root), "last-weekly"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func MarkWeekly(root string, now time.Time) error {
	if err := os.MkdirAll(AuditDir(root), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(AuditDir(root), "last-weekly"), []byte(now.Format("2006-01-02")+"\n"), 0600)
}

// WeeklyAudit は --since 7d --top 5 の見直しを作り、HTML を保存して、通知の decision を返す。
// 期間内の ask が 0 件なら何も作らず nil（この場合は確認した日を記録する）。
// decision を作れた場合の last-weekly の記録は、作成に成功した呼び出し側が MarkWeekly で行う。
func WeeklyAudit(root string, now time.Time, c Config, decisions []api.Decision) (*api.DecisionCreateParams, string, error) {
	asks, err := ReadAsks(AsksPath(root), now.Add(-7*24*time.Hour).Unix())
	if err != nil {
		return nil, "", err
	}
	rep, err := BuildAudit(AuditOptions{Root: root, Now: now, Since: 7 * 24 * time.Hour, Top: 5, Asks: asks, Decisions: decisions, Config: c, SampleLog: filepath.Join(root, "guard", "decisions.jsonl")})
	if err != nil {
		return nil, "", err
	}
	if rep.Total == 0 {
		return nil, "", MarkWeekly(root, now)
	}
	if err := os.MkdirAll(AuditDir(root), 0700); err != nil {
		return nil, "", err
	}
	path := filepath.Join(AuditDir(root), now.Format("2006-01-02")+".html")
	if err := os.WriteFile(path, []byte(RenderAuditHTML(rep)), 0600); err != nil {
		return nil, "", err
	}
	var body strings.Builder
	var opts []api.DecisionOption
	for n, b := range rep.Top {
		fmt.Fprintf(&body, "%d. %s — %d件・%s・%s\n", n+1, b.Name(), b.Count, outcomeText(b.Outcomes), b.Class)
		short := b.Name()
		opts = append(opts,
			api.DecisionOption{ID: "relax:" + strconv.Itoa(n+1), Label: "聞かなくする: " + short, Role: "other"},
			api.DecisionOption{ID: "tighten:" + strconv.Itoa(n+1), Label: "厳しくする: " + short, Role: "other"})
	}
	fmt.Fprintf(&body, "\n詳しい資料: %s\n選ばなかった束は「そのまま」です。", path)
	text := body.String()
	ms := uint64(24 * time.Hour / time.Millisecond)
	return &api.DecisionCreateParams{Kind: "ask", Title: fmt.Sprintf("今週のコマンド承認の見直し（%d件）", rep.Total), Body: &text, Options: opts, AllowText: true, MultiSelect: true, TimeoutMS: &ms}, path, nil
}
