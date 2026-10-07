package detect

import "testing"

func proc(pid int, name string, argv ...string) JobProcess {
	cmdline := ""
	for i, a := range argv {
		if i > 0 {
			cmdline += " "
		}
		cmdline += a
	}
	return JobProcess{PID: pid, Name: name, Argv: argv, Cmdline: cmdline}
}

func TestIdentifyJob(t *testing.T) {
	tests := []struct {
		name      string
		pgid      int
		processes []JobProcess
		wantID    string
		wantName  string
	}{
		{
			name: "leader is agent",
			pgid: 42,
			processes: []JobProcess{
				proc(42, "claude", "claude"),
				proc(43, "node", "node", "/tmp/mcp/bin/codex"),
			},
			wantID: "claude", wantName: "claude",
		},
		{
			name: "wrapped codex under node",
			pgid: 123,
			processes: []JobProcess{
				proc(1, "node", "node", "/path/to/bin/codex"),
				proc(2, "bash", "bash"),
			},
			wantID: "codex", wantName: "codex",
		},
		{
			name: "node running codex js script",
			pgid: 7,
			processes: []JobProcess{
				proc(7, "node", "/usr/local/bin/node", "/x/lib/node_modules/@openai/codex/bin/codex.js"),
			},
			wantID: "codex", wantName: "codex",
		},
		{
			name: "wrapped member beats unwrapped member by argv resolution",
			pgid: 42,
			processes: []JobProcess{
				proc(42, "bash", "bash"),
				proc(43, "node", "node", "/tmp/mcp/bin/codex"),
			},
			wantID: "codex", wantName: "codex",
		},
		{
			name: "argv-resolved agent beats direct binary member",
			pgid: 9,
			processes: []JobProcess{
				proc(9, "zsh", "zsh"),
				proc(10, "claude", "claude"),
				proc(11, "node", "node", "/opt/bin/devin"),
			},
			wantID: "devin", wantName: "devin",
		},
		{
			name: "sh -c does not resolve",
			pgid: 3,
			processes: []JobProcess{
				proc(3, "sh", "sh", "-c", "claude --verbose"),
			},
			wantID: "", wantName: "sh",
		},
		{
			name: "node eval does not resolve",
			pgid: 5,
			processes: []JobProcess{
				proc(5, "node", "node", "-e", "require('codex')"),
			},
			wantID: "", wantName: "node",
		},
		{
			name: "option value is skipped",
			pgid: 8,
			processes: []JobProcess{
				proc(8, "node", "node", "--require", "ts-node/register", "codex.js"),
			},
			wantID: "codex", wantName: "codex",
		},
		{
			name: "double dash resolves next token",
			pgid: 6,
			processes: []JobProcess{
				proc(6, "bun", "bun", "--smol", "--", "gemini"),
			},
			wantID: "gemini", wantName: "gemini",
		},
		{
			name: "argv0 path fallback",
			pgid: 4,
			processes: []JobProcess{
				proc(4, "claude", "/opt/homebrew/bin/claude"),
			},
			wantID: "claude", wantName: "claude",
		},
		{
			name: "cmdline fallback when argv missing",
			pgid: 2,
			processes: []JobProcess{
				{PID: 2, Name: "weird", Cmdline: "codex exec run"},
			},
			wantID: "codex", wantName: "codex",
		},
		{
			name: "plain shell reports comm as name",
			pgid: 1,
			processes: []JobProcess{
				proc(1, "zsh", "-zsh"),
			},
			wantID: "", wantName: "zsh",
		},
		{
			name: "wrapped non-agent keeps runtime name",
			pgid: 1,
			processes: []JobProcess{
				proc(1, "node", "node", "server.js"),
			},
			wantID: "", wantName: "node",
		},
		{
			name: "empty argv does not panic",
			pgid: 1,
			processes: []JobProcess{
				{PID: 1, Name: "node"},
			},
			wantID: "", wantName: "node",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotName := IdentifyJob(Job{
				ProcessGroupID: tt.pgid,
				Processes:      tt.processes,
			})
			if gotID != tt.wantID || gotName != tt.wantName {
				t.Errorf("IdentifyJob = (%q, %q), want (%q, %q)",
					gotID, gotName, tt.wantID, tt.wantName)
			}
		})
	}
}
