package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellGrammarAndPrefixesUseExistingRules(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "home", "..", "victim")
	// Keep the '..' spelling: the existing rm-paths rule explicitly asks for it.
	target = root + "/home/../victim"
	i := Input{Cwd: root, LaunchCwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin"}}
	cases := []struct{ line, want string }{
		{"/bin/rm " + target, "ask"},
		{"env -i /bin/rm " + target, "ask"},
		{"env -u IKESU_GUARD_MARKER rm " + target, "ask"},
		{"env --ignore-environment --unset=IKESU_PANE_ID X=x /bin/rm " + target, "ask"},
		{"env -S '/bin/rm " + target + "'", "ask"},
		{"env --split-string='/bin/rm " + target + "'", "ask"},
		{"builtin command /bin/rm " + target, "ask"},
		{"exec -a alias nohup nice -n 2 /bin/rm " + target, "ask"},
		{"time -p /bin/rm " + target, "ask"},
		{"false || /bin/rm " + target, "ask"},
		{"true && /bin/rm " + target, "ask"},
		{"printf ok; /bin/rm " + target, "ask"},
		{"printf ok | /bin/rm " + target, "ask"},
		{"if true; then /bin/rm " + target + "; fi", "ask"},
		{"while false; do /bin/rm " + target + "; done", "ask"},
		{"for x in 1; do /bin/rm " + target + "; done", "ask"},
		{"case x in x) /bin/rm " + target + ";; esac", "ask"},
		{"( /bin/rm " + target + " )", "ask"},
		{"printf '%s' $(/bin/rm " + target + ")", "ask"},
		{"printf '%s' `/bin/rm " + target + "`", "ask"},
		{"f() { /bin/rm " + target + "; }; true", "ask"},
		{"env -i /bin/zsh -c '/bin/rm " + target + "'", "ask"},
		{"env --unknown-option /bin/rm " + target, "ask"},
		{"builtin command \"$UNSET\"", "ask"},
		{"if ; then", "ask"},
		{"true || git push --force", "block"},
		{"env -S 'git push --force'", "block"},
		{"> .allow-main", "block"},
		{"ls", "allow"},
		{"git status", "allow"},
		{"cd /tmp", "allow"},
		{"printf ok | grep ok", "allow"},
		{"if true; then git status; else ls; fi", "allow"},
		{"for x in a b; do printf '%s' \"$x\"; done", "allow"},
		{"command -v rm", "allow"},
		{"env -u IKESU_GUARD_MARKER ls", "allow"},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, tc.line)
			if v.Decision != tc.want {
				t.Fatalf("got %s/%s: %s; want %s", v.Decision, v.Rule, v.Reason, tc.want)
			}
		})
	}
}

func TestShellAnalysisDoesNotExecuteSubstitutions(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "must-not-exist")
	i := Input{Cwd: root, Env: map[string]string{"HOME": root}}
	_, _, _ = lineVerdict(Config{}, i, "printf '%s' $(touch "+victim+") `touch "+victim+"`")
	if _, e := os.Stat(victim); !os.IsNotExist(e) {
		t.Fatal("analysis evaluated a command", e)
	}
}

func TestShellLineDangerAcknowledgementPreserved(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"AI_GUARD_DANGER_WORD_ACK": "1"}}
	_, v, e := lineVerdict(Config{}, i, "printf deploy")
	if e != nil || v.Decision != "allow" {
		t.Fatal(v, e)
	}
}
