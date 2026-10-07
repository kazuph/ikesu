package detect

import (
	"path/filepath"
	"strings"
)

// JobProcess is one member of a pane's foreground process group.
type JobProcess struct {
	PID     int
	Name    string
	Argv    []string
	Cmdline string
}

// Job is a pane's foreground process group.
type Job struct {
	ProcessGroupID int
	Processes      []JobProcess
}

// IdentifyJob returns the canonical agent id and the candidate name for a
// foreground job. Wrapped agents (e.g. codex launched as `node codex.js`)
// are resolved from the runtime's script argument. When no member is a
// known agent the returned id is empty and name is the group leader's
// comm for display purposes.
func IdentifyJob(job Job) (agentID, name string) {
	if len(job.Processes) == 0 {
		return "", ""
	}

	var leader *JobProcess
	for i := range job.Processes {
		if job.Processes[i].PID == job.ProcessGroupID {
			leader = &job.Processes[i]
			break
		}
	}
	if leader == nil {
		leader = &job.Processes[0]
	}

	leaderName := normalizedProcessName(*leader)
	if id := lookupAgent(normalizedLookupName(leaderName)); id != "" {
		return id, leaderName
	}

	bestID, bestName := "", ""
	bestScore := 0
	for _, pr := range job.Processes {
		cand := normalizedProcessName(pr)
		id := lookupAgent(normalizedLookupName(cand))
		if id == "" {
			continue
		}
		if s := processPriority(pr, cand); s > bestScore {
			bestScore, bestID, bestName = s, id, cand
		}
	}
	if bestID != "" {
		return bestID, bestName
	}
	return "", leader.Name
}

// normalizedProcessName resolves a process to its effective agent-candidate
// name: generic runtimes/shells resolve to their wrapped command, and bare
// paths resolve to an agent name when the basename (or the canonicalized
// basename, for symlinked shims) matches a manifest.
func normalizedProcessName(pr JobProcess) string {
	effective := ""
	if len(pr.Argv) > 0 {
		effective = pr.Argv[0]
	}
	if effective == "" {
		effective = pr.Name
	}

	if isGenericRuntimeOrShell(effective) {
		if wrapped := wrappedAgentNameFromRuntimeArgv(effective, pr.Argv); wrapped != "" {
			return wrapped
		}
	}

	if id := lookupAgent(normalizedLookupName(effective)); id != "" {
		return effective
	}

	if len(pr.Argv) > 0 {
		if w := agentNameFromPathToken(pr.Argv[0]); w != "" {
			return w
		}
	}
	if w := cmdlineArgv0AgentName(pr.Cmdline); w != "" {
		return w
	}
	return effective
}

// processPriority ranks agent candidates across a job's processes: a name
// resolved through argv beats comm, and a direct binary beats a generic
// runtime wrapper.
func processPriority(pr JobProcess, normalizedName string) int {
	if !strings.EqualFold(normalizedName, pr.Name) {
		return 3
	}
	if !isGenericRuntimeOrShell(normalizedName) {
		return 2
	}
	return 1
}

func isGenericRuntimeOrShell(name string) bool {
	switch normalizedLookupName(pathBase(name)) {
	case "sh", "bash", "zsh", "fish", "tmux", "node", "bun",
		"python", "python3", "cmd", "powershell", "pwsh":
		return true
	}
	return false
}

func wrappedAgentNameFromRuntimeArgv(runtime string, argv []string) string {
	switch normalizedLookupName(pathBase(runtime)) {
	case "node", "bun":
		return scriptArgAgentName(argv, []string{"-e", "--eval", "-p", "--print"}, nil)
	case "python", "python3":
		return scriptArgAgentName(argv, []string{"-c"}, []string{"-m"})
	case "sh", "bash", "zsh", "fish":
		return scriptArgAgentName(argv, []string{"-c"}, nil)
	case "cmd":
		return windowsCmdArgAgentName(argv)
	case "powershell", "pwsh":
		return powershellArgAgentName(argv)
	}
	return ""
}

// scriptArgAgentName returns the agent name of the first non-option argv
// entry (the script the runtime executes), skipping flags that consume a
// value. Eval/module invocations carry no script path and yield "".
func scriptArgAgentName(argv []string, evalFlags, moduleFlags []string) string {
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return agentNameFromPathToken(args[i+1])
			}
			return ""
		}
		if flagMatches(arg, evalFlags) || flagMatches(arg, moduleFlags) {
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			if optionTakesValue(arg) {
				i++
			}
			continue
		}
		return agentNameFromPathToken(arg)
	}
	return ""
}

func windowsCmdArgAgentName(argv []string) string {
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		flag := strings.ToLower(strings.Trim(args[i], `"'`))
		switch flag {
		case "/c", "/k":
			if i+1 < len(args) {
				return commandTextAgentName(args[i+1])
			}
			return ""
		case "/d", "/s", "/q", "/a", "/u", "/e:on", "/e:off",
			"/f:on", "/f:off", "/v:on", "/v:off":
			continue
		}
	}
	return ""
}

func powershellArgAgentName(argv []string) string {
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		flag := strings.ToLower(strings.Trim(args[i], `"'`))
		switch flag {
		case "-file", "-f", "/file":
			if i+1 < len(args) {
				return agentNameFromPathToken(args[i+1])
			}
			return ""
		case "-command", "-c", "/command", "/c":
			if i+1 < len(args) {
				return commandTextAgentName(args[i+1])
			}
			return ""
		case "-encodedcommand", "-enc", "/encodedcommand", "/enc":
			return ""
		case "-configurationname", "-executionpolicy", "-outputformat",
			"-psconsolefile", "-version", "-windowstyle", "-workingdirectory":
			i++
		default:
			if !strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "/") {
				return agentNameFromPathToken(args[i])
			}
		}
	}
	return ""
}

// commandTextAgentName picks the command token out of a shell command
// string, skipping leading &, . and call tokens.
func commandTextAgentName(command string) string {
	rest := command
	for {
		tok, next, ok := commandTextToken(rest)
		if !ok {
			return ""
		}
		rest = next
		t := strings.TrimSpace(tok)
		if strings.EqualFold(t, "&") || strings.EqualFold(t, ".") ||
			strings.EqualFold(t, "call") {
			continue
		}
		return agentNameFromPathToken(t)
	}
}

// commandTextToken returns the first whitespace- or quote-delimited token
// and the remainder of the input.
func commandTextToken(input string) (tok, rest string, ok bool) {
	input = strings.TrimLeft(input, " \t\r\n")
	if input == "" {
		return "", "", false
	}
	if q := input[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(input[1:], q); end >= 0 {
			return input[1 : 1+end], input[1+end+1:], true
		}
		return input[1:], "", true
	}
	end := strings.IndexAny(input, " \t\r\n")
	if end < 0 {
		end = len(input)
	}
	return input[:end], input[end:], true
}

func flagMatches(arg string, flags []string) bool {
	for _, f := range flags {
		if arg == f || shortFlagPayload(arg, f) || longFlagValue(arg, f) {
			return true
		}
	}
	return false
}

// shortFlagPayload matches a value glued to a short flag, e.g. "-efoo".
func shortFlagPayload(arg, flag string) bool {
	return strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") &&
		strings.HasPrefix(arg, flag) && len(arg) > len(flag)
}

// longFlagValue matches "--flag=value".
func longFlagValue(arg, flag string) bool {
	if !strings.HasPrefix(flag, "--") {
		return false
	}
	rest, ok := strings.CutPrefix(arg, flag)
	return ok && strings.HasPrefix(rest, "=")
}

func optionTakesValue(arg string) bool {
	switch arg {
	case "-r", "--require", "--loader", "--import", "--experimental-loader",
		"--inspect-port", "-W", "-X", "-S", "-L", "-o":
		return true
	}
	return false
}

func cmdlineArgv0AgentName(cmdline string) string {
	f := strings.Fields(cmdline)
	if len(f) == 0 {
		return ""
	}
	return agentNameFromPathToken(f[0])
}

// agentNameFromPathToken resolves a path or bare name to a canonical agent
// id, checking the basename, known package layouts, and finally the
// canonicalized path for symlinked shims (e.g. npm bin wrappers).
func agentNameFromPathToken(token string) string {
	trimmed := strings.Trim(token, `"'`)
	if trimmed == "" || strings.HasPrefix(trimmed, "-") {
		return ""
	}
	if id := lookupAgent(normalizedLookupName(pathBase(trimmed))); id != "" {
		return id
	}
	if id := agentNameFromKnownPackagePath(trimmed); id != "" {
		return id
	}
	if !strings.ContainsAny(trimmed, `/\`) {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil {
		return lookupAgent(normalizedLookupName(pathBase(resolved)))
	}
	return ""
}

func agentNameFromKnownPackagePath(path string) string {
	var comps []string
	for _, c := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if c != "" {
			comps = append(comps, normalizedLookupName(c))
		}
	}
	want := []string{"node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli"}
	for i := 0; i+len(want) <= len(comps); i++ {
		ok := true
		for j := range want {
			if comps[i+j] != want[j] {
				ok = false
				break
			}
		}
		if ok {
			return "pi"
		}
	}
	return ""
}

func pathBase(path string) string {
	base := path
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	if base == "" {
		return path
	}
	return base
}
