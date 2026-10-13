package plugin

import (
	"strings"
	"testing"
)

func TestPluginChildSessionScrub(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CODEX_THREAD_ID", "inherited")
	t.Setenv("HERDR_AGENT", "old")
	t.Setenv("GTX_GUARD_ENFORCING", "1")
	t.Setenv("CLAUDE_EFFORT", "high")
	e, p := actionEngine(t, minimal+`[[actions]]
id="env"
title="Env"
command=["/usr/bin/env"]
`)
	_, _, err := e.Invoke("", p.ID+".env", Context{})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	logs, err := e.Store.Logs(p.ID, nil)
	if err != nil || len(logs) != 1 || logs[0].Stdout == nil {
		t.Fatal(logs, err)
	}
	output := *logs[0].Stdout
	for _, key := range []string{"CLAUDE_CODE_CHILD_SESSION", "CODEX_THREAD_ID", "HERDR_AGENT", "GTX_GUARD_ENFORCING"} {
		if strings.Contains(output, key+"=") {
			t.Fatal(output)
		}
	}
	if !strings.Contains(output, "CLAUDE_EFFORT=high") {
		t.Fatal(output)
	}
}
