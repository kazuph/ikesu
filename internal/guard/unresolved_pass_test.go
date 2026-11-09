package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 「柵の中は聞かない。柵を越える操作だけ聞く」: 書き方が未確定なだけでは ask しない（2026-11-10）。
func TestUnresolvedAloneDoesNotAsk(t *testing.T) {
	d := t.TempDir()
	write := func(name, body string) {
		if e := os.WriteFile(filepath.Join(d, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("x.sh", "printf ok\n")
	write("deploy.sh", "wrangler deploy\n")
	write("mid.sh", "read V\necho $V > \"$V/a.txt\"\n\"$V\" arg\n")
	i := Input{Cwd: d, LaunchCwd: d, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": d}}
	for _, tc := range []struct{ line, want, rule string }{
		{`for s in 1 2 3; do PICKS="04:$s" bash x.sh; done`, "allow", ""},
		{`echo x > $S/a.txt`, "allow", ""},
		{`ONLY=1 bash mid.sh`, "allow", ""},
		{`$CMD args`, "allow", ""},
		{`for f in a b; do rm -rf "$f"; done`, "ask", "rm-paths"},
		{`rm -rf "$X"`, "ask", "rm-paths"},
		{`W=wrangler; $W deploy`, "ask", "danger-word-line"},
		{`bash deploy.sh`, "ask", ""},
		{`bash missing-unreadable.sh`, "allow", ""},
	} {
		t.Run(tc.line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.want || tc.rule != "" && v.Rule != tc.rule {
				t.Fatalf("got %+v want %s %s", v, tc.want, tc.rule)
			}
		})
	}
	t.Run("pass is marked", func(t *testing.T) {
		_, v, _ := lineVerdict(Config{}, i, `echo x > $S/a.txt`)
		if v.Rule != "default" || !strings.HasPrefix(v.Reason, undeterminedPrefix) {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("rm in script with unknown target asks", func(t *testing.T) {
		write("rmloop.sh", "for f in a b; do rm -rf \"$f\"; done\n")
		_, v, _ := lineVerdict(Config{}, i, "bash rmloop.sh")
		if v.Decision != "ask" || v.Rule != "rm-paths" {
			t.Fatalf("%+v", v)
		}
	})
}

func TestUnresolvedPassIsRecordedInGuardLog(t *testing.T) {
	i, _ := scriptPane(t)
	var out strings.Builder
	if !AuthorizeLine(Config{}, i.Cwd, i, `echo x > $S/a.txt`, &out, nil) {
		t.Fatalf("asked: %s", out.String())
	}
	log, e := os.ReadFile(filepath.Join(i.Cwd, "guard", "decisions.jsonl"))
	if e != nil || !strings.Contains(string(log), undeterminedPrefix) || !strings.Contains(string(log), `"rule":"default"`) {
		t.Fatalf("log=%s err=%v", log, e)
	}
}
