package server

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
	"github.com/kazuph/goterrace/internal/teststate"
	"strings"
	"testing"
)

func TestPaneAndAgentReadANSIThroughRealSocketCLI(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	waitAPISocket(t, dir)
	pid := firstPaneID(t, dir)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pid, Text: "printf '\\033[31mANSI_PROOF\\033[0m\\n'", Keys: []string{"Enter"}}, nil)
	timeout := uint64(10000)
	apiOK(t, dir, api.MethodPaneWaitForOutput, api.PaneWaitForOutputParams{PaneID: pid, Source: "recent", Match: api.OutputMatch{Type: "regex", Value: "^ANSI_PROOF$"}, TimeoutMs: &timeout}, nil)
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pid, Source: "test", Agent: "claude", State: "idle"}, nil)
	for _, command := range []string{"pane", "agent"} {
		for _, flag := range [][]string{{"--ansi"}, {"--format", "ansi"}} {
			var out, errs bytes.Buffer
			args := append([]string{command, "read", pid}, flag...)
			code := cli.Run(args, cli.Options{Stdout: &out, Stderr: &errs, Getenv: func(key string) string {
				if key == "GTX_SOCKET_PATH" {
					return api.APISocketPath(dir)
				}
				return ""
			}})
			if code != 0 || !strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "ANSI_PROOF") {
				t.Fatalf("%v: code=%d out=%q err=%s", args, code, out.String(), errs.String())
			}
		}
	}
}

func TestPaneAndAgentReadANSIStylesAndInvalidFormat(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "read-ansi")
	p, err := s.sup.SpawnWith(1, t.TempDir(), 8, 5, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	p.ReplayHistory("\x1b[31mabcdefghXYZ\x1b[m\r\nEND")
	for _, source := range []string{"visible", "recent", "recent_unwrapped"} {
		r := s.paneRead("x", api.PaneReadParams{PaneID: "p1", Source: source, Format: "ansi", StripANSI: true})
		if r.Err != nil {
			t.Fatal(r)
		}
		b, _ := json.Marshal(r.Result)
		var result api.PaneReadResultEnvelope
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if result.Read.Format != "ansi" || !strings.Contains(result.Read.Text, "\x1b[") || !strings.Contains(result.Read.Text, "\r\n") {
			t.Fatal(result)
		}
	}
	r := s.paneRead("x", api.PaneReadParams{PaneID: "p1", Format: "unsupported"})
	if r.Err == nil {
		t.Fatal("unknown format accepted")
	}
	// Agent read resolves to the same pane-reading contract.
	s.app.SetReported(1, "claude", "idle")
	r = s.agentRead("x", api.AgentReadParams{Target: "p1", Format: "ansi", StripANSI: true})
	if r.Err != nil {
		t.Fatal(r)
	}
	b, _ := json.Marshal(r.Result)
	if !strings.Contains(string(b), `\u001b`) {
		t.Fatal(string(b))
	}
}
