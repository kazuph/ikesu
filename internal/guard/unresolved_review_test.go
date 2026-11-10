package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// レビュー指摘（2026-11-10）: 未確定が混じっても外向き規則・guard の場所は効かせる。
func TestUnresolvedKeepsOutwardRules(t *testing.T) {
	root := t.TempDir()
	guard := filepath.Join(root, "guard")
	if e := os.MkdirAll(filepath.Join(guard, "agent-panes"), 0700); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(guard, "agent-panes", "p_1.json"), []byte("{}"), 0600)
	for name, body := range map[string]string{"dep.sh": "wrangler deploy\n", "pub.sh": "npm publish $1\n"} {
		os.WriteFile(filepath.Join(root, name), []byte(body), 0600)
	}
	i := Input{Cwd: root, LaunchCwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin", "IKESU_GUARD_MARKER": guard + "/agent-panes/p_1.json"}}
	for _, tc := range []struct{ line, want string }{
		{`bash dep.sh $X`, "ask"}, {`bash ./dep.sh "$X"`, "ask"}, {`bash dep.sh "$@"`, "ask"}, {`bash dep.sh $(date)`, "ask"},
		{`sh dep.sh $X`, "ask"}, {`bash -x dep.sh $X`, "ask"}, {`for f in a; do bash ./dep.sh $f; done`, "ask"},
		{`bash pub.sh $X`, "ask"}, {`for f in a; do bash ./pub.sh $f; done`, "ask"},
		{`$X curl -X POST https://a`, "ask"}, {`$X psql -c "DELETE FROM t"`, "ask"}, {`$X aws s3 rm s3://b`, "ask"},
		{`$X git push origin main`, "block"}, {`git -C $D push origin main`, "block"},
		{`git -C $D push --force origin x`, "block"}, {`git -C $D reset --hard`, "block"},
		{`cd $X; echo 1 > agent-panes/p_1.json`, "ask"}, {`echo 1 > $X/agent-panes/p_1.json`, "ask"},
		{`echo 1 > agent-panes/$X`, "ask"}, {`cd ..; echo 1 > guard/agent-panes/p_1.json`, "ask"}, {`cd $X; : > agent-panes/p_1.json`, "ask"},
		{`echo x > $S/a.txt`, "allow"}, {`echo 1 > "$X"`, "allow"}, {`echo 1 > $X`, "allow"},
		{`$X args`, "allow"}, {`bash x.sh $X`, "allow"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.want {
				t.Fatalf("got %+v want %s", v, tc.want)
			}
		})
	}
	os.WriteFile(filepath.Join(root, "x.sh"), []byte("printf ok\n"), 0600)
}

func TestUnresolvedLogIsBoundedAndDeduplicated(t *testing.T) {
	i, _ := scriptPane(t)
	long := `echo x > $S/a.txt # ` + strings.Repeat("a", 2000)
	for n := 0; n < 3; n++ {
		var out strings.Builder
		if !AuthorizeLine(Config{}, i.Cwd, i, long, &out, nil) {
			t.Fatal(out.String())
		}
	}
	log, e := os.ReadFile(filepath.Join(i.Cwd, "guard", "decisions.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 record, got %d", len(lines))
	}
	if len(lines[0]) > 3000 || strings.Contains(lines[0], strings.Repeat("a", 600)) {
		t.Fatalf("record not truncated: %d", len(lines[0]))
	}
}
