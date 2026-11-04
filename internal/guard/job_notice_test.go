package guard

import "testing"

func TestJobNoticeLineVerdict(t *testing.T) {
	line := "[ikesu run] exit=7 label=handoff job=j1"
	_, verdict, err := lineVerdict(Config{}, Input{Cwd: "/tmp", Env: map[string]string{"PATH": "/usr/bin:/bin"}}, line)
	if err != nil || verdict.Decision != "allow" {
		t.Fatalf("job notice line: verdict=%+v err=%v", verdict, err)
	}
}
