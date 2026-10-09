package server

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/platform"
	"testing"
)

func TestGrokNativeRestoreAndSafeLaunchObservation(t *testing.T) {
	testNativeRestore(t, "grok", "--resume")
	if Identify := detect.Identify("grok", "grok"); Identify != "grok" {
		t.Fatal(Identify)
	}
	if source := nativeResumeAgents["grok"].Source; source != "gtx:grok" {
		t.Fatal(source)
	}
}
func testNativeRestore(t *testing.T, agent, flag string) {
	t.Helper()
	s := &Server{cfg: config.Config{}}
	if cmd, ok := s.buildResumeCommand(agent, "id", "conversation-1"); !ok || cmd != agent+" "+flag+" conversation-1" {
		t.Fatalf("%q %v", cmd, ok)
	}
	for _, value := range []string{"", "--last", "last", "default:conversation-1", "one;two", "-option", "with space"} {
		if cmd, ok := s.buildResumeCommand(agent, "id", value); ok {
			t.Fatalf("unsafe %q produced %q", value, cmd)
		}
		job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{Name: agent, Argv: []string{agent, flag, value}}}}
		if id, ok := observeCmdline(job); ok {
			t.Fatalf("unsafe %q observed %q", value, id)
		}
	}
	if _, ok := s.buildResumeCommand(agent, "path", "/tmp/conversation.jsonl"); ok {
		t.Fatal("native agent accepted Path kind")
	}
	for _, args := range [][]string{{agent, flag, "conversation-1"}, {agent, flag + "=conversation-1"}} {
		job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{Name: agent, Argv: args}}}
		if id, ok := observeCmdline(job); !ok || id != "conversation-1" {
			t.Fatalf("%v: %q %v", args, id, ok)
		}
	}
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{Name: agent, Argv: []string{agent}}}}
	if _, ok := observeCmdline(job); ok {
		t.Fatal("bare launch supplied a session")
	}
}
