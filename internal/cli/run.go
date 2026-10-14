package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/machine"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

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
	var label, cwd, caller, completion, split, machineRef string
	syncRemote := false
	paneMode := false
	closeOnSuccess := false
	var argv []string
	i := 0
	for i < len(args) && argv == nil {
		switch args[i] {
		case "--":
			argv = append([]string{}, args[i+1:]...)
		case "--machine":
			v, ok := flagValue(args, &i)
			if !ok || v == "" {
				fmt.Fprintln(r.errOut(), "missing value for --machine")
				return 2
			}
			machineRef = v
		case "--sync":
			syncRemote = true
			i++
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
	if syncRemote && machineRef == "" {
		fmt.Fprintln(r.errOut(), "--sync requires --machine")
		return 2
	}
	if machineRef != "" {
		if label == "" {
			label = defaultRunLabel(argv)
		}
		remote, err := r.remoteRun(machineRef, syncRemote, argv, cwd, paneMode)
		if err != nil {
			fmt.Fprintln(r.errOut(), remoteRunError(err))
			return 2
		}
		argv = remote
		cwd = ""
	}
	if paneMode {
		return r.runPaneCLI(argv, label, cwd, split, closeOnSuccess, caller)
	}
	return r.runBackgroundCLI(argv, label, cwd, caller, completion)
}

// runSpawnOutput is the one-line JSON both `gtx run` modes print — same
// shape as herdr's RunSpawnOutput so callers (and AI agents) can read the
// job id and the "wait for the notice" hint without parsing prose.
type runSpawnOutput struct {
	Job   string  `json:"job"`
	Label string  `json:"label"`
	Mode  string  `json:"mode"`
	Pane  *string `json:"pane"`
	Next  string  `json:"next"`
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
	out, _ := json.Marshal(runSpawnOutput{
		Job: res.Job, Label: res.Label, Mode: res.Mode,
		Next: runSpawnNextHint(res.Job, res.Label, nil, completion),
	})
	fmt.Fprintln(r.out(), string(out))
	return 0
}

// runPaneCLI opens a split pane and types `gtx __pane-run ...` inside it.
// The wrapper runs the command, logs to job-logs, sends the caller pane a
// gtx-jobs completion nudge, and closes its own pane on success when
// --close-on-success was given — mirroring herdr's run --pane contract.
func (r *runner) runPaneCLI(argv []string, label, cwd, split string, closeOnSuccess bool, explicitCaller string) int {
	caller := explicitCaller
	var err error
	if caller == "" {
		caller, err = r.resolveCurrentPaneID()
	}
	if err != nil {
		fmt.Fprintln(r.errOut(), err.Error())
		return 1
	}
	rawCaller, e := r.request("cli:run:caller", api.MethodPaneGet, api.PaneTarget{PaneID: caller})
	var callerResponse api.Response
	if e != nil || json.Unmarshal(rawCaller, &callerResponse) != nil || callerResponse.Err != nil {
		fmt.Fprintln(r.errOut(), "gtx run --pane: caller pane could not be resolved")
		return 1
	}
	var callerInfo api.PaneInfoResult
	if json.Unmarshal(callerResponse.Result, &callerInfo) != nil || !dispatch.IsGlobalPaneID(callerInfo.Pane.PaneID) {
		return usageErr(r, "invalid caller pane response")
	}
	caller = callerInfo.Pane.PaneID
	callerAgent := caller
	if label == "" {
		label = defaultRunLabel(argv)
	}
	job := newPaneJobID()
	if split == "" {
		split = "right"
	}
	raw, err := r.request("cli:run:split", api.MethodPaneSplit, api.PaneSplitParams{
		TargetPaneID: &caller, Direction: split, Cwd: optPtr(cwd), Focus: false,
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
	store, e := dispatch.Open(dispatch.DBPath(r.o.Session))
	if e != nil {
		fmt.Fprintln(r.errOut(), e)
		return 1
	}
	jobCwd := cwd
	if jobCwd == "" && res.Pane.Cwd != nil {
		jobCwd = *res.Pane.Cwd
	}
	e = store.InsertCommand(job, label, shellCommandFromArgs(argv), jobCwd, caller, caller, "summary", dispatch.JobLogPath(job, label, r.o.Session))
	store.Close()
	if e != nil {
		fmt.Fprintln(r.errOut(), e)
		return 1
	}

	_, _ = r.request("cli:run:rename", api.MethodPaneRename,
		api.PaneRenameParams{PaneID: paneID, Label: &label})

	bin, err := os.Executable()
	if err != nil {
		bin = "gtx"
		if p, e := exec.LookPath("gtx"); e == nil {
			bin = p
		}
	}
	parts := []string{
		shellQuote(bin), "--session", shellQuote(r.o.Session), "__pane-run",
		"--parent-agent", shellQuote(callerAgent),
		"--target", shellQuote(paneID),
		"--job-id", shellQuote(job),
		"--run-label", shellQuote(label),
	}
	if closeOnSuccess {
		parts = append(parts, "--close-on-success")
	} else {
		parts = append(parts, "--close-on-exit")
	}
	parts = append(parts, "--", shellQuote(shellCommandFromArgs(argv)))
	code := r.sendOK(api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: paneID, Text: strings.Join(parts, " "), Keys: []string{"Enter"},
	})
	if code != 0 {
		return code
	}
	out, _ := json.Marshal(runSpawnOutput{
		Job: job, Label: label, Mode: "pane",
		Next: runSpawnNextHint(job, label, &paneID, "summary"), Pane: &paneID,
	})
	fmt.Fprintln(r.out(), string(out))
	return 0
}

// callerAgentName resolves the mailbox/agent name of a pane (custom name
// first, then detected agent, like herdr's resolve_agent_name).
func callerAgentName(r *runner, paneID string) string {
	info, apiErr := r.agentGetRaw(paneID)
	if apiErr != nil || info == nil {
		return ""
	}
	if info.Name != nil && *info.Name != "" {
		return *info.Name
	}
	if info.Agent != nil && *info.Agent != "" {
		return *info.Agent
	}
	return ""
}

// newPaneJobID mirrors herdr's job-<unix_ms>-<pid>-<seq> ids: pane-mode
// jobs are tracked by their log file only (no dispatch row), like herdr.
var paneJobSeq uint32

func newPaneJobID() string {
	n := atomic.AddUint32(&paneJobSeq, 1)
	return fmt.Sprintf("job-%d-%d-%d", time.Now().UnixMilli(), os.Getpid(), n)
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
	store, err := dispatch.OpenReadOnly(dispatch.DBPath(r.o.Session))
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
			fmt.Fprintln(r.out(), dispatch.DBPath(r.o.Session))
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
	logPath := ""
	if store, err := dispatch.OpenReadOnly(dispatch.DBPath(r.o.Session)); err == nil {
		if job, jerr := store.CommandRow(jobID); jerr == nil && job != nil {
			printRunListJob(r.out(), *job)
			if job.CallerAgent != "" {
				fmt.Fprintf(r.out(), "caller: %s\n", job.CallerAgent)
			}
			logPath = job.LogPath
		}
		store.Close()
	}
	if logPath == "" {
		// Pane-mode `run --pane` jobs have no dispatch row (same as herdr):
		// their record is the log file job-logs/<id>-<label>.log.
		if !validPaneJobID(jobID) {
			fmt.Fprintf(r.errOut(), "job not found: %s\n", jobID)
			return 1
		}
		matches, _ := filepath.Glob(filepath.Join(dispatch.JobLogDir(r.o.Session), jobID+"-*.log"))
		if len(matches) == 0 {
			fmt.Fprintf(r.errOut(), "job not found: %s\n", jobID)
			return 1
		}
		logPath = matches[0]
		fmt.Fprintf(r.out(), "%s (pane job)\n", jobID)
	}
	data, err := os.ReadFile(logPath)
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

// paneRunWrapper is the `gtx __pane-run` wrapper typed into the job pane
// by `gtx run --pane`. It runs the command via `sh -lc`, tees output to
// the job log, sends the caller a gtx-jobs completion nudge, and closes
// the pane per --close-on-success / --close-on-exit (herdr's __pane-run).
func (r *runner) paneRunWrapper(args []string) int {
	var target, jobID, label string
	closeOnSuccess, closeOnExit := false, false
	i := 0
	for i < len(args) && args[i] != "--" {
		switch args[i] {
		case "--parent-agent":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --parent-agent")
				return 2
			}
			_ = v
		case "--target":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --target")
				return 2
			}
			target = v
		case "--job-id":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --job-id")
				return 2
			}
			jobID = v
		case "--run-label":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --run-label")
				return 2
			}
			label = v
		case "--close-on-success":
			closeOnSuccess = true
			i++
		case "--close-on-exit":
			closeOnExit = true
			i++
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if i >= len(args) || args[i] != "--" || i+1 >= len(args) ||
		jobID == "" || target == "" || !validPaneJobID(jobID) {
		fmt.Fprintln(r.errOut(), "usage: gtx __pane-run --target PANE --job-id ID [--parent-agent NAME] [--run-label L] [--close-on-success|--close-on-exit] -- <command...>")
		return 2
	}
	command := strings.Join(args[i+1:], " ")
	if label == "" {
		label = defaultRunLabel(args[i+1:])
	}

	logPath := dispatch.JobLogPath(jobID, label, r.o.Session)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintf(r.errOut(), "gtx __pane-run: cannot create log dir: %v\n", err)
		return 1
	}
	log, err := os.Create(logPath)
	if err != nil {
		fmt.Fprintf(r.errOut(), "gtx __pane-run: cannot create log: %v\n", err)
		return 1
	}
	defer log.Close()
	if store, err := dispatch.Open(dispatch.DBPath(r.o.Session)); err == nil {
		if job, e := store.CommandRow(jobID); e == nil && job != nil {
			_ = store.MarkCommandRunning(jobID, int64(os.Getpid()))
		}
		store.Close()
	}
	started := time.Now()
	fmt.Fprintf(log, "== gtx run %s label=%s started=%s cmd=%s\n\n",
		jobID, label, started.Format(time.RFC3339), command)

	profiles := r.catalog().Load()
	marker := "gtx-ssh-connected-" + rand.Text()
	machineID, observed := machine.ObservePaneRun(profiles, command, marker, shellCommandFromArgs)
	cmd := childenv.Command("/bin/sh", "-lc", observed)
	cmd.Env = childenv.Scrub(os.Environ())
	cmd.Stdin = os.Stdin
	wout, werr := io.MultiWriter(os.Stdout, log), io.MultiWriter(os.Stderr, log)
	cmd.Stdout, cmd.Stderr = wout, werr
	var approval *machine.RunApproval
	var approvalOut, approvalErr *machine.ApprovalStream
	if machineID != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		approval = &machine.RunApproval{ID: machineID, Marker: marker, Expire: func() {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}}
		approvalOut = &machine.ApprovalStream{State: approval, Out: wout}
		approvalErr = &machine.ApprovalStream{State: approval, Out: werr}
		cmd.Stdout, cmd.Stderr = approvalOut, approvalErr
	}
	code := runCmdExitCode(cmd)
	if approval != nil {
		_ = approvalOut.Close()
		_ = approvalErr.Close()
		approval.Close()
	}

	fmt.Fprintf(log, "\n== gtx run %s exit=%d finished=%s\n",
		jobID, code, time.Now().Format(time.RFC3339))

	{
		line := fmt.Sprintf("[gtx run] exit=%d label=%s pane=%s details: gtx log %s",
			code, oneLineField(label), target, jobID)
		caller := ""
		if store, err := dispatch.Open(dispatch.DBPath(r.o.Session)); err == nil {
			if job, e := store.CommandRow(jobID); e == nil && job != nil {
				caller = job.CallerPane
				exit := int64(code)
				_, _ = store.MarkCommandFinished(jobID, &exit)
			}
			store.Close()
		}
		r.notifyPaneJob(caller, line)
	}
	if closeOnExit || (closeOnSuccess && code == 0) {
		if paneID, err := r.resolveCurrentPaneID(); err == nil {
			_, _ = r.request("cli:pane-run:close", api.MethodPaneClose,
				api.PaneTarget{PaneID: paneID})
			return code
		}
		_, _ = r.request("cli:pane-run:close", api.MethodPaneClose,
			api.PaneTarget{PaneID: target})
		return code
	}
	// Keep the pane alive for inspection: replace the wrapper with a
	// login shell (herdr --pane leaves the pane open on failure / without
	// a close flag).
	if sh := os.Getenv("SHELL"); sh != "" {
		_ = syscall.Exec(sh, []string{sh, "-l"}, childenv.Scrub(os.Environ()))
	}
	return code
}

// validPaneJobID accepts the id shapes a pane runner may log under:
// herdr-style job-<ms>-<pid>-<seq> and the dispatch jNNNN ids.
var paneJobIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validPaneJobID(id string) bool {
	return len(id) <= 200 && paneJobIDRe.MatchString(id)
}

// runCmdExitCode runs a command and returns its exit code (128+signal for
// signalled processes, like a shell; 1 for a spawn failure).
func runCmdExitCode(cmd *exec.Cmd) int {
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			if c := ee.ExitCode(); c >= 0 {
				return c
			}
		}
		return 1
	}
	return 0
}

// A stopped server cannot confirm disappearance, so only a proven ID queues.
func (r *runner) notifyPaneJob(caller, body string) {
	params := api.MsgSendParams{Room: dispatch.JobsRoom, FromAgent: dispatch.RunShellActor, To: caller, Body: body}
	if _, err := r.request("cli:pane-run:notify", api.MethodMsgSend, params); err == nil {
		return
	}
	store, err := dispatch.Open(dispatch.DBPath(r.o.Session))
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return
	}
	defer store.Close()
	if !dispatch.IsGlobalPaneID(caller) {
		_ = store.InsertFailedMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, caller, body, "job notification has no immutable caller pane", nil)
		return
	}
	_, err = store.InsertMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, caller, body, nil, false)
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
	}
}
