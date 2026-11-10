package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWeekly(t *testing.T) {
	for in, want := range map[string]WeeklySpec{
		"":          {Day: time.Monday, Hour: 10},
		"mon 10:00": {Day: time.Monday, Hour: 10},
		"SUN 23:59": {Day: time.Sunday, Hour: 23, Min: 59},
		"fri 9:05":  {Day: time.Friday, Hour: 9, Min: 5},
		"off":       {Off: true},
		" Off ":     {Off: true},
	} {
		got, err := ParseWeekly(in)
		if err != nil || got != want {
			t.Errorf("ParseWeekly(%q)=%+v %v want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"monday 10:00", "mon", "mon 25:00", "mon 10:60", "mon 10", "mon 10:00 x"} {
		if _, err := ParseWeekly(bad); err == nil {
			t.Errorf("ParseWeekly(%q) should fail", bad)
		}
	}
}

// 2026-11-10 は火曜。月曜 10:00 の枠は 11-09 10:00。
func TestWeeklyDue(t *testing.T) {
	w, _ := ParseWeekly("mon 10:00")
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.Local) }
	for _, tc := range []struct {
		name string
		now  time.Time
		last string
		want bool
	}{
		{"月曜10時の前（先週分は済み）", at(9, 9, 59), "2026-11-02", false},
		{"月曜10時ちょうど", at(9, 10, 0), "2026-11-02", true},
		{"同じ週に2回作らない", at(9, 10, 1), "2026-11-09", false},
		{"同じ週の火曜も作らない", at(10, 12, 0), "2026-11-09", false},
		{"起動が遅れて火曜に取り戻す", at(10, 12, 0), "2026-11-02", true},
		{"何週も止まっていても取り戻す（1回だけ）", at(10, 12, 0), "2026-10-05", true},
		{"取り戻した日は次の月曜まで作らない", at(15, 9, 0), "2026-11-10", false},
		{"次の月曜10時で作る", at(16, 10, 0), "2026-11-10", true},
		{"記録が無ければ作る", at(10, 12, 0), "", true},
		{"壊れた記録は作る", at(10, 12, 0), "きのう", true},
	} {
		if got := w.Due(tc.now, tc.last); got != tc.want {
			t.Errorf("%s: Due=%v want %v", tc.name, got, tc.want)
		}
	}
	if slot := w.LastSlot(at(10, 12, 0)); !slot.Equal(at(9, 10, 0)) {
		t.Errorf("LastSlot=%v", slot)
	}
	if slot := w.LastSlot(at(9, 9, 0)); !slot.Equal(at(2, 10, 0)) {
		t.Errorf("LastSlot before slot=%v", slot)
	}
	sun, _ := ParseWeekly("sun 10:00") // 2026-11-07 は土曜、11-08 が日曜
	if sun.Due(at(7, 9, 0), "2026-11-01") || !sun.Due(at(8, 10, 0), "2026-11-01") {
		t.Error("sunday slot")
	}
	off, _ := ParseWeekly("off")
	if off.Due(at(9, 10, 0), "") {
		t.Error("off must never be due")
	}
}

func TestWeeklyAuditCreatesDecisionAndHTML(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	// 0件なら何もせず、確認した日だけ記録する。
	if p, path, err := WeeklyAudit(root, now, Config{}, nil); err != nil || p != nil || path != "" {
		t.Fatalf("empty: %v %q %v", p, path, err)
	}
	if got := ReadLastWeekly(root); got != "2026-11-10" {
		t.Fatalf("last-weekly=%q", got)
	}
	if _, err := os.Stat(AuditDir(root)); err != nil {
		t.Fatal(err)
	}
	for _, a := range []AskRecord{
		ask("http-write", "curl -X POST https://e.com", OutcomeApprove, time.Hour),
		ask("http-write", "curl -X POST https://e.com", OutcomeApprove, 2*time.Hour),
		ask("shell-line", "bash -c x", OutcomeApprove, time.Hour),
	} {
		a.Repo = "" // 日付はこのテストの now に合わせる
		a.TSEpoch = now.Unix() - 3600
		if err := AppendAsk(root, a); err != nil {
			t.Fatal(err)
		}
	}
	p, path, err := WeeklyAudit(root, now, Config{}, nil)
	if err != nil || p == nil {
		t.Fatalf("%v %v", p, err)
	}
	if path != filepath.Join(root, "guard", "audit", "2026-11-10.html") {
		t.Fatalf("path=%s", path)
	}
	html, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(html), "コマンド承認の見直し") {
		t.Fatalf("html: %v", err)
	}
	if p.Kind != "ask" || p.Title != "今週のコマンド承認の見直し（3件）" || !p.MultiSelect || !p.AllowText || p.TimeoutMS == nil || *p.TimeoutMS != 24*3600*1000 {
		t.Fatalf("params=%+v", p)
	}
	if len(p.Options) != 4 || p.Options[0].ID != "relax:1" || p.Options[1].ID != "tighten:1" || p.Options[2].ID != "relax:2" || !strings.HasPrefix(p.Options[0].Label, "聞かなくする: http-write・curl・-") || !strings.HasPrefix(p.Options[1].Label, "厳しくする: ") {
		t.Fatalf("options=%+v", p.Options)
	}
	body := *p.Body
	if !strings.Contains(body, "1. http-write・curl・- — 2件・approve 2・妥当（外向き）") || !strings.Contains(body, path) {
		t.Fatalf("body=%s", body)
	}
	// 決定を作った側が MarkWeekly するまでは、まだ番が来たまま。
	w, _ := ParseWeekly("")
	if !w.Due(now, "2026-11-02") {
		t.Fatal("must stay due until marked")
	}
	if err := MarkWeekly(root, now); err != nil || w.Due(now, ReadLastWeekly(root)) {
		t.Fatalf("after mark: %v", err)
	}
}
