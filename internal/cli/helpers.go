package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
)

// parseEnvAssignment splits "KEY=VALUE"; the key may not be empty.
func parseEnvAssignment(s string) (key, value string, err error) {
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return "", "", fmt.Errorf("invalid env assignment %q (want KEY=VALUE)", s)
	}
	return s[:i], s[i+1:], nil
}

// parseReadSource accepts the documented --source values and normalizes
// the hyphenated alias of recent_unwrapped.
func parseReadSource(v string) (string, error) {
	switch v {
	case "visible", "recent", "recent_unwrapped", "detection":
		return v, nil
	case "recent-unwrapped":
		return "recent_unwrapped", nil
	}
	return "", fmt.Errorf("invalid read source: %s", v)
}

// parseSplitDirection accepts the split directions pane split/run --pane
// understand.
func parseSplitDirection(v string) (string, error) {
	switch v {
	case "right", "down":
		return v, nil
	}
	return "", fmt.Errorf("invalid split direction: %s (want right or down)", v)
}

// parseU32Flag parses a non-negative 32-bit flag value.
func parseU32Flag(flag, value string) (uint32, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value: %s", flag, value)
	}
	return uint32(n), nil
}

// parseU64Flag parses a non-negative 64-bit flag value.
func parseU64Flag(flag, value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value: %s", flag, value)
	}
	return n, nil
}

// resolveAgentName resolves a pane target to the agent name it reports:
// display name first, then agent type, then the global pane id.
func (r *runner) resolveAgentName(target string) (string, error) {
	a, e := r.agentGetRaw(target)
	if e != nil {
		return "", fmt.Errorf("%s", e.Message)
	}
	switch {
	case a.Name != nil && *a.Name != "":
		return *a.Name, nil
	case a.Agent != nil && *a.Agent != "":
		return *a.Agent, nil
	case a.GlobalPaneID != "":
		return a.GlobalPaneID, nil
	}
	return "", fmt.Errorf("%s has no reported agent identity", target)
}

// resolveCurrentPaneID resolves this CLI's own pane, preferring the
// IKESU_PANE_ID env var as caller.
func (r *runner) resolveCurrentPaneID() (string, error) {
	var caller *string
	if env := r.envPaneID(); env != "" {
		caller = &env
	}
	return r.currentPaneID(caller)
}

// printMsgMessage prints one inbox/history line:
// #<id> [<room>] <created_at> <from> -> <to>: <body>
func printMsgMessage(w io.Writer, m api.MsgMessage) {
	fmt.Fprintf(w, "#%d [%s] %s %s -> %s: %s\n",
		m.ID, m.Room, m.CreatedAt, m.FromAgent, m.ToAgent, m.Body)
}

func printSendHelp(r *runner) {
	fmt.Fprintln(r.errOut(), "usage: ikesu send <to> <text> [--room R] [--reply-to ID] [--from NAME]")
	fmt.Fprintln(r.errOut(), "sends a message to an agent or pane; --room selects the room (default \"default\")")
	fmt.Fprintln(r.errOut(), "  --reply-to links this message to a received dispatch id")
	fmt.Fprintln(r.errOut(), "  --from overrides the sender name (default: your pane's reported name)")
}

func printMsgHelp(r *runner) {
	fmt.Fprintln(r.errOut(), "ikesu msg commands:")
	fmt.Fprintln(r.errOut(), "  ikesu send <to> <text> [--room R] [--reply-to ID] [--from NAME]")
	fmt.Fprintln(r.errOut(), "  ikesu inbox [--room R] [--to NAME]           # queued messages delivered to you")
	fmt.Fprintln(r.errOut(), "  ikesu msg history [--room R] [--project P] [--limit N]")
	fmt.Fprintln(r.errOut(), "  ikesu msg rooms                              # rooms with activity, recent first")
}

func printLogHelp(r *runner) {
	fmt.Fprintln(r.errOut(), "ikesu log commands:")
	fmt.Fprintln(r.errOut(), "  ikesu log [--room R] [--project P] [--limit N]   # dispatch timeline, oldest first")
	fmt.Fprintln(r.errOut(), "  ikesu log <job_id> [--tail N]                    # output log of a ikesu run job")
	fmt.Fprintln(r.errOut(), "  ikesu log --db                                   # SQLite database path (WAL; safe to query)")
	fmt.Fprintln(r.errOut(), "  ikesu log --schema                               # database schema")
	fmt.Fprintln(r.errOut(), "lowest-level API: sqlite3 \"$(ikesu log --db)\"")
}

func printRunHelp(r *runner) {
	fmt.Fprintln(r.errOut(), "ikesu run commands:")
	fmt.Fprintln(r.errOut(), "  ikesu run [--label L] [--cwd D] [--completion summary|full|none] -- <command...>")
	fmt.Fprintln(r.errOut(), "  ikesu run --pane [--split right|down] [--close-on-success] -- <command...>")
	fmt.Fprintln(r.errOut(), "  ikesu run --machine <label-or-id> [--sync] [run options] -- <command...>")
	fmt.Fprintln(r.errOut(), "  ikesu run list                # active jobs")
	fmt.Fprintln(r.errOut(), "  ikesu run cancel <job_id>     # request cancellation of a queued/running job")
	fmt.Fprintln(r.errOut(), "waiting: do not sleep or poll for the job. When it exits, the caller receives a")
	fmt.Fprintln(r.errOut(), "  \"[ikesu run] exit=<code> label=<label> job=<id>\" notice (pane mode: pane=<pane>);")
	fmt.Fprintln(r.errOut(), "  end your turn and wait for it, then read the output with \"ikesu log <job>\".")
}

// defaultRunLabel is the label a `ikesu run` gets without --label: the last
// path component of the command, "run" when there is none.
func defaultRunLabel(argv []string) string {
	if len(argv) == 0 {
		return "run"
	}
	base := argv[0]
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if base == "" || base == "." {
		return "run"
	}
	return base
}

// oneLineField trims surrounding whitespace and folds newlines to spaces.
func oneLineField(s string) string {
	t := strings.TrimSpace(s)
	t = strings.ReplaceAll(t, "\r\n", " ")
	t = strings.ReplaceAll(t, "\n", " ")
	return strings.ReplaceAll(t, "\r", " ")
}

// runSpawnNextHint is the "next" instruction the spawn response hands to
// the calling agent.
func runSpawnNextHint(job, label string, pane *string, completion string) string {
	switch {
	case pane != nil:
		return fmt.Sprintf(
			"a [ikesu run] exit=<code> label=%s pane=%s notice will reach the caller; wait for it without sleeping or polling, then read the result with \"ikesu log %s\"",
			label, *pane, job)
	case completion == "none":
		return fmt.Sprintf(
			"no completion notice is sent; check progress yourself with \"ikesu run list\" or \"ikesu log %s\"",
			job)
	default:
		return fmt.Sprintf(
			"a [ikesu run] exit=<code> label=%s job=%s notice will reach the caller; end your turn and wait without sleeping or polling, then read the result with \"ikesu log %s\"",
			label, job, job)
	}
}

// shellCommandFromArgs reconstructs one shell command line from argv so
// that a POSIX shell parses it back to the same arguments.
func shellCommandFromArgs(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// shellQuote renders s as one POSIX-shell word: unquoted when every byte
// is shell-safe, ” when empty, else single-quoted with the '\” idiom.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("@%_+=:,./-~", c) >= 0:
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
