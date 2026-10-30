package guard

import "testing"

func TestEvalDirectoryChangeAffectsSiblings(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin"}}
	for _, tc := range []struct{ wrapped, direct string }{
		{`eval 'cd /some/where'; rm valuable`, `cd /some/where; rm valuable`},
		{`eval 'cd /some/where' && rm valuable`, `cd /some/where && rm valuable`},
		{"eval 'cd /some/where'\nrm valuable", "cd /some/where\nrm valuable"},
		{`eval 'cd /some/where'; echo x > relative.txt`, `cd /some/where; echo x > relative.txt`},
		{`eval "eval 'cd /some/where'"; rm valuable`, `cd /some/where; rm valuable`},
		{`eval 'pushd /some/where'; rm valuable`, `pushd /some/where; rm valuable`},
	} {
		t.Run(tc.wrapped, func(t *testing.T) {
			_, got, _ := lineVerdict(Config{}, i, tc.wrapped)
			_, want, _ := lineVerdict(Config{}, i, tc.direct)
			if got.Decision != "ask" || got.Decision != want.Decision {
				t.Fatalf("wrapped=%+v direct=%+v", got, want)
			}
		})
	}
}
