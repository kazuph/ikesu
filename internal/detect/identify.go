package detect

import (
	"path/filepath"
	"strings"
)

// JobProcess is one process of a pane's foreground job.
type JobProcess struct {
	PID     int
	Name    string
	Argv    []string
	Cmdline string
}

// Job is the foreground process group running in a pane.
type Job struct {
	ProcessGroupID int
	Processes      []JobProcess
}

// agentNames is the name→canonical-id table: lowercased accepted names
// (suffix-stripped by normalizedLookupName at lookup) map to the official
// agent id. It is intentionally separate from the detection manifests —
// names like omp or mastracode identify an agent that has no rules yet.
var agentNames = map[string]string{
	"pi":              "pi",
	"claude":          "claude",
	"claude-code":     "claude",
	"codex":           "codex",
	"gemini":          "gemini",
	"cursor":          "cursor",
	"cursor-agent":    "cursor",
	"devin":           "devin",
	"devin-cli":       "devin",
	"devin cli":       "devin",
	"agy":             "agy",
	"antigravity":     "agy",
	"antigravity-cli": "agy",
	"cline":           "cline",
	"omp":             "omp",
	"mastracode":      "mastracode",
	"mastra-code":     "mastracode",
	"mastra code":     "mastracode",
	"opencode":        "opencode",
	"open-code":       "opencode",
	"copilot":         "copilot",
	"github-copilot":  "copilot",
	"ghcs":            "copilot",
	"kimi":            "kimi",
	"kimi-code":       "kimi",
	"kimi code":       "kimi",
	"kiro":            "kiro",
	"kiro-cli":        "kiro",
	"droid":           "droid",
	"amp":             "amp",
	"amp-local":       "amp",
	"grok":            "grok",
	"grok-build":      "grok",
	"hermes":          "hermes",
	"hermes-agent":    "hermes",
	"kilo":            "kilo",
	"kilo-code":       "kilo",
	"kilo code":       "kilo",
	"qodercli":        "qodercli",
	"qoderclicn":      "qodercli",
	"qoder":           "qodercli",
	"qodercn":         "qodercli",
	"maki":            "maki",
	"muse":            "muse",
	"muse-bin":        "muse",
	"muse-code":       "muse",
	"muse-cli":        "muse",
	"qwen":            "qwen",
	"qwen-code":       "qwen",
	"qwen code":       "qwen",
	"letta":           "letta",
	"letta-code":      "letta",
	"letta code":      "letta",
}

// agentIDFor looks a name up in the agent table (case-insensitive,
// whitespace-trimmed, one executable suffix removed).
func agentIDFor(name string) string {
	return agentNames[normalizedLookupName(name)]
}

// genericRuntimes are process names that only host another program; the
// real agent hides in their arguments.
var genericRuntimes = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "tmux": true,
	"node": true, "bun": true, "python": true, "python3": true,
	"cmd": true, "powershell": true, "pwsh": true,
}

// IdentifyJob returns the canonical agent id ("" when none matches) and a
// display name for the job running in a pane.
func IdentifyJob(job Job) (agentID, name string) {
	if len(job.Processes) == 0 {
		return "", ""
	}
	cands := make([]string, len(job.Processes))
	for i := range job.Processes {
		cands[i] = processCandidate(job.Processes[i])
	}
	// The process-group leader decides the agent when it is one.
	leader := -1
	for i := range job.Processes {
		if job.Processes[i].PID == job.ProcessGroupID {
			leader = i
			break
		}
	}
	if leader >= 0 {
		if id := agentIDFor(cands[leader]); id != "" {
			return id, cands[leader]
		}
	}
	// Otherwise the highest-scoring agent candidate wins; ties keep order.
	best, bestScore := -1, 0
	for i := range job.Processes {
		if agentIDFor(cands[i]) == "" {
			continue
		}
		score := 1
		switch {
		case !strings.EqualFold(cands[i], job.Processes[i].Name):
			score = 3 // found inside a wrapper's arguments
		case !genericRuntimes[cands[i]]:
			score = 2
		}
		if best < 0 || score > bestScore {
			best, bestScore = i, score
		}
	}
	if best >= 0 {
		return agentIDFor(cands[best]), cands[best]
	}
	fallback := leader
	if fallback < 0 {
		fallback = 0
	}
	return "", job.Processes[fallback].Name
}

// processCandidate reduces one process to the name that may identify an
// agent: the exec name itself, or the wrapped agent's canonical id.
func processCandidate(p JobProcess) string {
	exec := execName(p)
	if genericRuntimes[exec] {
		if id, ok := wrappedAgent(exec, p); ok {
			return id
		}
		return exec
	}
	if agentIDFor(exec) != "" {
		return exec
	}
	word := ""
	if len(p.Argv) > 0 {
		word = p.Argv[0]
	}
	if word == "" {
		if f := strings.Fields(p.Cmdline); len(f) > 0 {
			word = f[0]
		}
	}
	if id, ok := agentFromPathWord(word); ok {
		return id
	}
	return exec
}

// execName is the process's program name: Argv[0]'s basename when argv is
// available, else the reported process name.
func execName(p JobProcess) string {
	s := p.Name
	if len(p.Argv) > 0 && p.Argv[0] != "" {
		s = p.Argv[0]
		if i := strings.LastIndexAny(s, `/\`); i >= 0 {
			s = s[i+1:]
		}
	}
	return normalizedLookupName(s)
}

// wrappedAgent inspects a runtime's arguments for the program it runs.
func wrappedAgent(runtime string, p JobProcess) (string, bool) {
	var args []string
	if len(p.Argv) > 1 {
		args = p.Argv[1:]
	}
	switch runtime {
	case "node", "bun":
		return scanScriptArgs(args, nodeEvalFlag)
	case "python", "python3":
		return scanScriptArgs(args, pythonEvalFlag)
	case "sh", "bash", "zsh", "fish":
		return scanScriptArgs(args, shellEvalFlag)
	case "cmd":
		return cmdWrappedAgent(args)
	case "powershell", "pwsh":
		return pwshWrappedAgent(args)
	}
	return "", false // tmux and anything else
}

// valueFlags consume the following argument as their value.
var valueFlags = map[string]bool{
	"-r": true, "--require": true, "--loader": true, "--import": true,
	"--experimental-loader": true, "--inspect-port": true,
	"-W": true, "-X": true, "-S": true, "-L": true, "-o": true,
}

// scanScriptArgs walks interpreter arguments shared by node/bun, python,
// and POSIX shells: eval/module flags mean "not an agent", "--" defers to
// the next word, flags are skipped (value flags eat one more word), and
// the first plain word is resolved as a program path.
func scanScriptArgs(args []string, evalFlag func(string) bool) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 >= len(args) {
				return "", false
			}
			return agentFromPathWord(args[i+1])
		}
		if evalFlag(a) {
			return "", false
		}
		if strings.HasPrefix(a, "-") {
			if valueFlags[a] {
				i++
			}
			continue
		}
		return agentFromPathWord(a)
	}
	return "", false
}

// nodeEvalFlag reports inline-eval/print flags (also the attached forms
// -eX, -pX, --eval=X, --print=X).
func nodeEvalFlag(a string) bool {
	if strings.HasPrefix(a, "--") {
		return a == "--eval" || a == "--print" ||
			strings.HasPrefix(a, "--eval=") || strings.HasPrefix(a, "--print=")
	}
	return a == "-e" || a == "-p" ||
		strings.HasPrefix(a, "-e") || strings.HasPrefix(a, "-p")
}

// pythonEvalFlag reports -c command / -m module (attached forms too).
func pythonEvalFlag(a string) bool {
	if strings.HasPrefix(a, "--") {
		return false
	}
	return strings.HasPrefix(a, "-c") || strings.HasPrefix(a, "-m")
}

// shellEvalFlag reports -c command (attached form too).
func shellEvalFlag(a string) bool {
	return !strings.HasPrefix(a, "--") && strings.HasPrefix(a, "-c")
}

// cmdMetaFlags are cmd.exe switches that carry no program argument.
var cmdMetaFlags = map[string]bool{
	"/d": true, "/s": true, "/q": true, "/a": true, "/u": true,
	"/e:on": true, "/e:off": true, "/f:on": true, "/f:off": true,
	"/v:on": true, "/v:off": true,
}

// cmdWrappedAgent resolves `cmd /c <command line>` / `/k`.
func cmdWrappedAgent(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		w := strings.ToLower(strings.Trim(args[i], `"`))
		switch {
		case w == "/c" || w == "/k":
			if i+1 >= len(args) {
				return "", false
			}
			return agentFromCommandLine(args[i+1])
		case cmdMetaFlags[w]:
			continue
		}
	}
	return "", false
}

// pwshValueFlags consume the following argument as their value.
var pwshValueFlags = map[string]bool{
	"-configurationname": true, "-executionpolicy": true,
	"-outputformat": true, "-psconsolefile": true, "-version": true,
	"-windowstyle": true, "-workingdirectory": true,
}

// pwshWrappedAgent resolves -File/-Command and rejects encoded commands.
func pwshWrappedAgent(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		w := strings.ToLower(strings.Trim(args[i], `"`))
		switch w {
		case "-file", "-f", "/file":
			if i+1 >= len(args) {
				return "", false
			}
			return agentFromPathWord(args[i+1])
		case "-command", "-c", "/command", "/c":
			if i+1 >= len(args) {
				return "", false
			}
			return agentFromCommandLine(args[i+1])
		case "-encodedcommand", "-enc", "/encodedcommand", "/enc":
			return "", false
		}
		if pwshValueFlags[w] {
			i++
			continue
		}
		if strings.HasPrefix(w, "-") || strings.HasPrefix(w, "/") {
			continue
		}
		return agentFromPathWord(args[i])
	}
	return "", false
}

// commandLineWords splits a Windows command line into words: a word
// starting with a quote runs to the matching quote, others to whitespace.
func commandLineWords(s string) []string {
	var words []string
	i := 0
	for i < len(s) {
		for i < len(s) && strings.IndexByte(" \t\r\n", s[i]) >= 0 {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		if s[i] == '"' || s[i] == '\'' {
			q := s[i]
			i++
			for i < len(s) && s[i] != q {
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			for i < len(s) && strings.IndexByte(" \t\r\n", s[i]) < 0 {
				i++
			}
		}
		words = append(words, s[start:i])
	}
	return words
}

// agentFromCommandLine resolves the command named by a command line: `&`,
// `.` and `call` are skipped, the first remaining word wins.
func agentFromCommandLine(s string) (string, bool) {
	for _, w := range commandLineWords(s) {
		if w == "&" || w == "." || strings.EqualFold(w, "call") {
			continue
		}
		return agentFromPathWord(w)
	}
	return "", false
}

// piAgentPath is the node_modules suffix identifying the pi agent.
var piAgentPath = []string{
	"node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli",
}

// agentFromPathWord resolves a program word to a canonical agent id:
// basename lookup first, then the pi node_modules path signature, then a
// symlink-resolved basename for real paths.
func agentFromPathWord(w string) (string, bool) {
	w = strings.Trim(w, `"'`)
	if w == "" || strings.HasPrefix(w, "-") {
		return "", false
	}
	if id := agentIDFor(pathBaseName(w)); id != "" {
		return id, true
	}
	// The pi agent ships as node_modules/@earendil-works/pi-coding-agent/dist/cli.js
	parts := strings.FieldsFunc(w, func(r rune) bool { return r == '/' || r == '\\' })
outer:
	for i := 0; i+len(piAgentPath) <= len(parts); i++ {
		for j, want := range piAgentPath {
			if normalizedLookupName(parts[i+j]) != want {
				continue outer
			}
		}
		return "pi", true
	}
	if strings.ContainsAny(w, `/\`) {
		if resolved, err := filepath.EvalSymlinks(w); err == nil && resolved != w {
			if id := agentIDFor(pathBaseName(resolved)); id != "" {
				return id, true
			}
		}
	}
	return "", false
}

// pathBaseName normalizes a path's last component for the agent table:
// the listed executable suffixes first, then any leftover extension
// (e.g. codex.py → codex).
func pathBaseName(p string) string {
	base := p
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if n := normalizedLookupName(base); agentIDFor(n) != "" {
		return n
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return normalizedLookupName(base[:i])
	}
	return normalizedLookupName(base)
}
