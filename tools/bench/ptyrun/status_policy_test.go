package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDroppedStatusPolicySurvivesTimelineReceiver(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "ptyrun")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	steps := filepath.Join(dir, "steps")
	timeline := filepath.Join(dir, "timeline.jsonl")
	if err := os.WriteFile(steps, []byte("waittext 5s READY\nkill\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-status-delay", "drop", "-timeline", timeline, "-steps", steps, "--", "/bin/sh", "-c", `stty raw -echo; printf '\033[5nREADY'; exec /bin/cat`).CombinedOutput()
	if err != nil {
		t.Fatalf("PTY run: %v %s", err, out)
	}
	if !strings.Contains(string(out), "status-query at_unix_us=") {
		t.Fatal("timeline replaced the explicitly requested drop policy", string(out))
	}
	data, err := os.ReadFile(timeline)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "terminal.status_complete") {
		t.Fatal("dropped status unexpectedly received an automatic reply")
	}
}
