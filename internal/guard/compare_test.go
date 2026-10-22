package guard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareStatusesWindowsAndMalformed(t *testing.T) {
	d := t.TempDir()
	g, a := filepath.Join(d, "g"), filepath.Join(d, "a")
	var b strings.Builder
	for n, r := range []Record{{Decision: "allow", CommandLine: "agree"}, {Decision: "ask", CommandLine: "diverge"}, {Decision: "ask", CommandLine: "reject"}, {Decision: "ask", CommandLine: "day"}, {Decision: "block", CommandLine: "block"}, {Decision: "allow", CommandLine: "only"}, {Decision: "allow", CommandLine: "far"}} {
		r.Epoch = 1767225600 + int64(n)
		r.Rule = "test"
		j, _ := json.Marshal(r)
		b.Write(j)
		b.WriteByte('\n')
	}
	b.WriteString("not-json\n")
	os.WriteFile(g, []byte(b.String()), 0600)
	os.WriteFile(a, []byte("2026-01-01T00:00:00Z\tALLOW\tagree\n2026-01-01T00:00:01Z\tALLOW\tdiverge\n2026-01-01T00:00:02Z\tREJECT\treject\n2026-01-01T00:00:03Z\tDAY_ALLOW\tday\n2026-01-01T00:00:04Z\tBLOCK_PROTECTED\tblock\n2026-01-01T00:00:00Z\tALLOW\tai-only\n2027-01-01T00:00:06Z\tALLOW\tfar\nmalformed\n"), 0600)
	zero := int64(0)
	for _, window := range []*int64{nil, &zero} {
		var out bytes.Buffer
		code, e := Compare(CompareOptions{GuardLog: g, AILog: a, Window: window}, &out)
		if e != nil || code != 1 {
			t.Fatal(code, e)
		}
		for _, s := range []string{"AGREE", "DIVERGE", "IKESU-ONLY", "AI-GUARD-ONLY", "malformed: ikesu=1 ai-guard=1"} {
			if !strings.Contains(out.String(), s) {
				t.Fatal(s, out.String())
			}
		}
		if strings.Contains(out.String(), "OUT-OF-WINDOW") != (window != nil) {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	code, e := Compare(CompareOptions{GuardLog: g, AILog: a, Since: 1767225700}, &out)
	if e != nil || code != 0 {
		t.Fatal(code, e)
	}
}
func TestCompareInclusiveWindowAndOffset(t *testing.T) {
	d := t.TempDir()
	g, a := filepath.Join(d, "g"), filepath.Join(d, "a")
	os.WriteFile(g, []byte(`{"ts_epoch":1767225600,"cmd_line":"echo hi","verdict":"ask"}`+"\n"), 0600)
	os.WriteFile(a, []byte("2026-01-01T09:00:01+09:00\tPROMPT_SHOWN\techo hi\n"), 0600)
	for _, n := range []int64{0, 1} {
		var out bytes.Buffer
		code, e := Compare(CompareOptions{GuardLog: g, AILog: a, Window: &n}, &out)
		if e != nil || code != 0 {
			t.Fatal(code, e)
		}
		if strings.Contains(out.String(), "OUT-OF-WINDOW") != (n == 0) {
			t.Fatal(out.String())
		}
	}
}
