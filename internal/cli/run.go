package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
)

// `gtx run` (background + pane jobs), `gtx run list|cancel`, `gtx log`
// (job log viewer + dispatch timeline + --db/--schema). `run list` and
// `log` read the store directly — like herdr they keep working when no
// server is running.

func (r *runner) runCLI(args []string) int {
	if len(args) == 0 {
		printRunHelp(r)
		return 2
	}
	switch args[0] {
	case "list":
		return r.runListCLI(args[1:])
	case "cancel":
		if len(args) < 2 {
			fmt.Fprintln(r.errOut(), "usage: gtx run cancel <job_id>")
			return 2
		}
		return r.runCancelCLI(args[1])
	case "help", "--help", "-h":
		printRunHelp(r)
		return 0
	}

	// `gtx run [flags] -- <command...>` — herdr also accepts the command
	// without a bare `--` separator.
	var label, cwd, caller, completion, split string
	paneMode := false
	closeOnSuccess := false
	var argv []string
	i := 0
	for i < len(args) && argv == nil {
		switch args[i] {
		case "--":
			argv = append([]string{}, args[i+1:]...)
		case "--label":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --label")
				return 2
			}
			label = v
		case "--cwd":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --cwd")
				return 2
			}
			cwd = v
		case "--caller":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --caller")
				return 2
			}
			caller = v
		case "--completion":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --completion")
				return 2
			}
			completion = v
		case "--split":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --split")
				return 2
			}
			if v != "right" && v != "down" {
				fmt.Fprintln(r.errOut(), "--split must be right or down")
				return 2
			}
			split = v
		case "--pane":
			paneMode = true
			i++
		case "--close-on-success":
			closeOnSuccess = true
			i++
		default:
			argv = append([]string{}, args[i:]...)
		}
	}
	if len(argv) == 0 {
		printRunHelp(r)
		return 2
	}
	if paneMode {
		return r.runPaneCLI(argv, label, cwd, split, closeOnSuccess)
	}
	return r.runBackgroundCLI(argv, label, cwd, caller, completion)
}

// runBackgroundCLI submits a pane-less job via run.start.
func (r *runner) runBackgroundCLI(argv []string, label, cwd, caller, completion string) int {
	if caller == "" {
		paneID, err := r.resolveCurrentPaneID()
		if err != nil {
			fmt.Fprintln(r.errOut(), err.Error())
			return 1
		}
		caller = paneID
	}
	raw, err := r.request("cli:run:start", api.MethodRunStart, api.RunStartParams{
		CallerPane: caller, Label: label, Cwd: cwd, Argv: argv, Completion: completion,
	})
	if err != nil {
		raw = r.serverNotRunning("cli:run:start")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res api.RunStartedResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	fmt.Fprintf(r.out(), "job %s started (mode=%s)\n", res.Job, res.Mode)
	if res.Label != "" {
		fmt.Fprintf(r.out(), "label: %s\n", res.Label)
	}
	return 0
}

// runPaneCLI opens a split pane and types the command inside the
// `gtx __pane-run` wrapper, which reports the exit and closes the pane on
// success when --close-on-success was given (otherwise it drops into an
// interactive shell so the output stays readable).
func (r *runner) runPaneCLI(argv []string, label, cwd, split string, closeOnSuccess bool) int {
	if split == "" {
		split = "right"
	}
	var caller *string
	if env := r.envPaneID(); env != "" {
		caller = &env
	}
	raw, err := r.request("cli:run:split", api.MethodPaneSplit, api.PaneSplitParams{
		TargetPaneID: caller, Direction: split, Cwd: optPtr(cwd), Focus: true,
		Env: map[string]string{},
	})
	if err != nil {
		raw = r.serverNotRunning("cli:run:split")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res api.PaneInfoResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	paneID := res.Pane.PaneID
	bin := "gtx"
	if p, e := exec.LookPath("gtx"); e == nil {
		bin = p
	}
	line := shellQuote(bin) + " __pane-run"
	if closeOnSuccess {
		line += " --close-on-success"
	}
	line += " --"
	for _, a := range argv {
		line += " " + shellQuote(a)
	}
	if label != "" {
		_, _ = r.request("cli:run:rename", api.MethodPaneRename,
			api.PaneRenameParams{PaneID: paneID, Label: &label})
	}
	return r.sendOK(api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: paneID, Text: line, Keys: []string{"Enter"},
	})
}

// shellQuote renders one argv element for POSIX shell parsing.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=' || r == '+' || r == ',' || r == '@' || r == '%')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func optPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *runner) runCancelCLI(jobID string) int {
	raw, err := r.request("cli:run:cancel", api.MethodRunCancel, api.RunCancelParams{JobID: jobID})
	if err != nil {
		raw = r.serverNotRunning("cli:run:cancel")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res api.RunCancelledResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	fmt.Fprintf(r.out(), "job %s %s (signal=%s)\n", res.Job, res.Status, res.Signal)
	return 0
}

// runListCLI prints jobs newest first, straight from the store.
func (r *runner) runListCLI(args []string) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(r.errOut(), "usage: gtx run list")
		return 0
	}
	store, err := dispatch.OpenReadOnly(dispatch.DBPath())
	if err != nil {
		fmt.Fprintf(r.errOut(), "gtx: cannot open store: %v\n", err)
		return 1
	}
	defer store.Close()
	jobs, err := store.CommandRows(0)
	if err != nil {
		fmt.Fprintf(r.errOut(), "gtx: %v\n", err)
		return 1
	}
	if len(jobs) == 0 {
		fmt.Fprintln(r.out(), "no jobs")
		return 0
	}
	for _, j := range jobs {
		printRunListJob(r.out(), j)
	}
	return 0
}

func printRunListJob(w interface{ Write([]byte) (int, error) }, j dispatch.Job) {
	exit := "-"
	if j.ExitCode != nil {
		exit = fmt.Sprintf("%d", *j.ExitCode)
	}
	label := j.Label
	if label == "" {
		label = "-"
	}
	fmt.Fprintf(w, "%s %s exit=%s label=%s cmd=%s\n", j.ID, j.Status, exit, label, j.Command)
}

// --- gtx log ---

func (r *runner) logCLI(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "--db":
			fmt.Fprintln(r.out(), dispatch.DBPath())
			return 0
		case "--schema":
			fmt.Fprint(r.out(), dispatch.Schema())
			return 0
		case "help", "--help", "-h":
			printLogHelp(r)
			return 0
		}
		if !strings.HasPrefix(args[0], "-") {
			return r.logJobCLI(args)
		}
	}
	// Dispatch timeline (gtx log with only filter flags / no args).
	room, project, limit, code := r.parseHistoryArgs(args)
	if code >= 0 {
		return code
	}
	return r.printMessages("cli:msg:history", api.MethodMsgHistory,
		api.MsgHistoryParams{Room: room, Project: project, Limit: limit}, "log")
}

// logJobCLI prints a job's metadata and its log file tail.
func (r *runner) logJobCLI(args []string) int {
	jobID := args[0]
	tail := 50
	i := 1
	for i < len(args) {
		if args[i] == "--tail" {
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --tail")
				return 2
			}
			n, err := parseU32Flag("--tail", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err.Error())
				return 2
			}
			tail = int(n)
		} else {
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	store, err := dispatch.OpenReadOnly(dispatch.DBPath())
	if err != nil {
		fmt.Fprintf(r.errOut(), "gtx: cannot open store: %v\n", err)
		return 1
	}
	defer store.Close()
	job, err := store.CommandRow(jobID)
	if err != nil || job == nil {
		fmt.Fprintf(r.errOut(), "job not found: %s\n", jobID)
		return 1
	}
	printRunListJob(r.out(), *job)
	if job.CallerAgent != "" {
		fmt.Fprintf(r.out(), "caller: %s\n", job.CallerAgent)
	}
	if job.LogPath == "" {
		return 0
	}
	data, err := os.ReadFile(job.LogPath)
	if err != nil {
		fmt.Fprintf(r.errOut(), "gtx: cannot read log: %v\n", err)
		return 1
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	for _, l := range lines {
		fmt.Fprintln(r.out(), l)
	}
	return 0
}

// --- internal: __pane-run ---

// paneRunWrapper executes argv inside a pane and, with --close-on-success,
// closes its own pane when the command exits 0.
func (r *runner) paneRunWrapper(args []string) int {
	closeOnSuccess := false
	i := 0
	for i < len(args) && args[i] != "--" {
		if args[i] == "--close-on-success" {
			closeOnSuccess = true
		}
		i++
	}
	if i >= len(args) || args[i] != "--" || i+1 >= len(args) {
		fmt.Fprintln(r.errOut(), "usage: gtx __pane-run [--close-on-success] -- <command...>")
		return 2
	}
	argv := args[i+1:]
	code := runPassthrough(argv)
	if closeOnSuccess && code == 0 {
		if paneID, err := r.resolveCurrentPaneID(); err == nil {
			_, _ = r.request("cli:pane-run:close", api.MethodPaneClose,
				api.PaneTarget{PaneID: paneID})
		}
		return code
	}
	// Keep the pane alive for inspection: replace the wrapper with a
	// login shell (herdr --pane leaves the pane open on failure / without
	// --close-on-success).
	if sh := os.Getenv("SHELL"); sh != "" {
		_ = syscall.Exec(sh, []string{sh, "-l"}, os.Environ())
	}
	return code
}

// runPassthrough runs a command with inherited stdio and returns its exit
// code (128+signal for signalled processes, like a shell).
func runPassthrough(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(interface{ ExitCode() int }); ok {
			if c := ee.ExitCode(); c >= 0 {
				return c
			}
		}
		return 1
	}
	return 0
}
