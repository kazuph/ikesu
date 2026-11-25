package snapshot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotRedactsStructuredSecretsAndPEM(t *testing.T) {
	t.Parallel()
	const secret = "SNAPSHOT_TEST_ONLY"
	lines := []string{
		"› investigate",
		`{"passWord": "SNAPSHOT_TEST_ONLY\"quoted", "visible": "ok", "ACCESS_TOKEN":"SNAPSHOT_TEST_ONLY"}`,
		"passwd: SNAPSHOT_TEST_ONLY with spaces, punctuation",
		"custom_key = 'SNAPSHOT_TEST_ONLY'",
		"_token = 'SNAPSHOT_TEST_ONLY'",
		"private_key=-----BEGIN OPENSSH PRIVATE KEY-----",
		secret,
		"-----END OPENSSH PRIVATE KEY-----",
		"retained",
	}
	var log string
	result := Build(context.Background(), Input{
		Agent: "codex", Cwd: "/test", Mode: "handoff", Lines: lines,
		SaveLog: func(text string) (string, error) { log = text; return "/tmp/example.log", nil },
		Git: func(_ context.Context, _ string, args ...string) (string, error) {
			switch args[0] {
			case "rev-parse":
				if args[1] == "--is-inside-work-tree" {
					return "true", nil
				}
				return "token: " + secret, nil
			case "status", "diff", "log":
				return strings.Join(lines, "\n"), nil
			}
			t.Errorf("unexpected command %q", args)
			return "", nil
		},
	})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"json": string(data), "markdown": result.Markdown, "log": log} {
		if strings.Contains(text, secret) || strings.Contains(text, "PRIVATE KEY-----") ||
			!strings.Contains(text, "retained") || !strings.Contains(text, "***") {
			t.Fatalf("%s invalid masking: %q", name, text)
		}
	}
	if !strings.Contains(result.LastTurn, `"visible": "ok"`) || !strings.Contains(result.LastTurn, `"passWord": "***"`) {
		t.Fatalf("quoted masking damaged adjacent fields: %s", result.LastTurn)
	}
}

func TestSnapshotMasksPrivateKeyCrossingReadBoundary(t *testing.T) {
	t.Parallel()
	lines := make([]string, MaxLines+1)
	lines[0] = "-----BEGIN ED25519 PRIVATE KEY-----"
	copy(lines[1:], []string{"PEM_BODY_ONE", "PEM_BODY_TWO", "-----END ED25519 PRIVATE KEY-----", "visible after key"})
	lines[len(lines)-2] = "-----BEGIN PRIVATE KEY-----"
	lines[len(lines)-1] = "UNTERMINATED_BODY"

	result, log := buildRedactionFixture(lines)
	assertRedactedSnapshot(t, result, log, "visible after key", "PEM_BODY_ONE", "PEM_BODY_TWO", "UNTERMINATED_BODY", "PRIVATE KEY-----")
	if !strings.HasPrefix(log, "***\n***\n***\nvisible after key") || !strings.HasSuffix(log, "***\n***") {
		t.Fatalf("wrong PEM boundaries: %q", log)
	}
}

func TestSnapshotMasksYAMLQuotedAndBlockSecrets(t *testing.T) {
	t.Parallel()
	lines := []string{
		"› inspect",
		"password: 'first''SECOND_PART'",
		"token: |",
		"  BLOCK_FIRST",
		"",
		"  BLOCK_SECOND",
		"visible: retained",
		"  client_secret: >2- # folded",
		"     DEEP_BLOCK",
		"  same_indent: retained",
		"    token: |+3",
		"       NESTED_BLOCK",
		"shallower_indent: retained",
	}
	result, log := buildRedactionFixture(lines)
	for _, visible := range []string{"visible: retained", "same_indent: retained", "shallower_indent: retained"} {
		assertRedactedSnapshot(t, result, log, visible, "first", "SECOND_PART", "BLOCK_FIRST", "BLOCK_SECOND", "DEEP_BLOCK", "NESTED_BLOCK")
	}
	if !strings.Contains(log, "password: '***'\ntoken: ***\n***\n\n***\nvisible: retained") {
		t.Fatalf("wrong YAML boundaries: %q", log)
	}
}

func buildRedactionFixture(lines []string) (Result, string) {
	var log string
	result := Build(context.Background(), Input{
		Agent: "codex", Mode: "handoff", Lines: lines, FallbackLines: MaxLines,
		SaveLog: func(text string) (string, error) { log = text; return "/tmp/snapshot.log", nil },
	})
	return result, log
}

func assertRedactedSnapshot(t *testing.T, result Result, log, retained string, secrets ...string) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"markdown": result.Markdown, "last turn": result.LastTurn, "saved log": log, "json": string(data),
	} {
		if !strings.Contains(output, retained) {
			t.Errorf("%s did not retain %q: %q", name, retained, output)
		}
		for _, secret := range secrets {
			if strings.Contains(output, secret) {
				t.Errorf("%s retains %q: %q", name, secret, output)
			}
		}
	}
}
