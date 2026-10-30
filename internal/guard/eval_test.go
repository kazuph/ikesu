package guard

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestObservedClaudeEvalMatchesDirect(t *testing.T) {
	f, err := os.Open("testdata/claude-eval-observed.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	count := 0
	for s.Scan() {
		var entry struct {
			Cmd  []string `json:"cmd"`
			Line string   `json:"cmd_line"`
			Cwd  string   `json:"cwd"`
		}
		if err := json.Unmarshal(s.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		count++
		i := Input{Cwd: entry.Cwd, LaunchCwd: entry.Cwd, Env: map[string]string{"PATH": "/usr/bin:/bin"}}
		gotInput, got, err := lineVerdict(Config{}, i, entry.Line)
		wantInput, want, wantErr := lineVerdict(Config{}, i, entry.Cmd[1])
		if err != nil || wantErr != nil || got.Decision != want.Decision || got.Rule != want.Rule || gotInput.AuthorizationEnv["AI_GUARD_REQUEST_REASON"] != wantInput.AuthorizationEnv["AI_GUARD_REQUEST_REASON"] {
			t.Errorf("case %d: got %+v reason %q err %v; want %+v reason %q err %v", count, got, gotInput.AuthorizationEnv["AI_GUARD_REQUEST_REASON"], err, want, wantInput.AuthorizationEnv["AI_GUARD_REQUEST_REASON"], wantErr)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("got %d records", count)
	}
}

func TestEvalRejectsDynamicAndDeep(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "x": "pwd", "b": "pwd"}}
	deep := "pwd"
	for n := 0; n <= scriptDepthLimit; n++ {
		deep = "eval " + quote(deep)
	}
	for _, line := range []string{`eval "$x"`, `eval $(cmd)`, `eval 'a'"$b"`, `eval *`, deep} {
		_, v, _ := lineVerdict(Config{}, i, line)
		if v.Decision == "allow" {
			t.Errorf("allowed %s: %+v", line, v)
		}
	}
	_, malformed, _ := lineVerdict(Config{}, i, `eval 'if'`)
	if malformed.Decision == "allow" {
		t.Errorf("allowed malformed eval in runtime: %+v", malformed)
	}
	_, v, _ := lineVerdict(Config{}, i, "eval "+quote("ikesu send '*' hello"))
	if v.Decision != "block" {
		t.Errorf("prohibited command: %+v", v)
	}
}
