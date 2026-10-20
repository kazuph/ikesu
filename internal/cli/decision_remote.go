package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kazuph/ikesu/internal/machine"
)

const decisionSSHAuthPrefix = "To authenticate, visit:"

func (r *runner) remoteDecisionBinary() string {
	if bin := r.o.getenv("GTX_MACHINE_REMOTE_BINARY"); bin != "" {
		return bin
	}
	return "gtx"
}
func decisionAuthReason(line string) (string, bool) {
	_, url, ok := strings.Cut(line, decisionSSHAuthPrefix)
	if !ok {
		return "", false
	}
	return "SSH authentication required: " + strings.TrimSpace(url), true
}
func (r *runner) decisionMachineAnswer(args []string) (bool, int) {
	ref := ""
	clean := []string{}
	found := false
	for i := 0; i < len(args); {
		if args[i] == "--machine" {
			if found {
				return true, usageErr(r, "--machine may only be specified once")
			}
			var ok bool
			ref, ok = flagValue(args, &i)
			if !ok || ref == "" {
				return true, usageErr(r, "--machine requires a saved machine label or id")
			}
			found = true
		} else if strings.HasPrefix(args[i], "--machine=") {
			if found {
				return true, usageErr(r, "--machine may only be specified once")
			}
			ref = strings.TrimPrefix(args[i], "--machine=")
			if ref == "" {
				return true, usageErr(r, "--machine requires a saved machine label or id")
			}
			found = true
			i++
		} else {
			clean = append(clean, args[i])
			if args[i] == "--text" || args[i] == "--responder" {
				if i+1 >= len(args) {
					return true, usageErr(r, "missing option value")
				}
				clean = append(clean, args[i+1])
				i += 2
			} else {
				i++
			}
		}
	}
	if !found {
		return false, 0
	}
	if ref == "local" {
		return true, r.decisionCLI(append([]string{"answer"}, clean...))
	}
	p, err := machine.Resolve(r.catalog().Load(), ref, true)
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return true, 1
	}
	cmd := childenv.Command("ssh", machine.ForwardArgs(p, r.remoteDecisionBinary(), append([]string{"decision", "answer"}, clean...))...)
	cmd.Stdout, cmd.Stderr = r.out(), r.errOut()
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return true, e.ExitCode()
		}
		fmt.Fprintln(r.errOut(), err)
		return true, 1
	}
	return true, 0
}
func (r *runner) watchRemote(ctx context.Context, p machine.Profile, lines chan<- decisionWatchLine) {
	owner := decisionMachine{ID: p.ID, Label: p.Label}
	delay := time.Duration(0)
	unsupported := false
	for ctx.Err() == nil {
		start := time.Now()
		result := r.watchRemoteConnection(ctx, p, owner, lines)
		reason, auth := result.reason, result.auth
		if result.observed {
			unsupported = false
		}
		if ctx.Err() != nil {
			return
		}
		notify, next := remoteWatchRetry(&unsupported, delay, result)
		if notify && !sendWatch(ctx, lines, decisionWatchLine{Event: "machine_down", Machine: owner, Reason: reason}) {
			return
		}
		if time.Since(start) >= decisionRetryStable {
			delay = 0
		}
		if !unsupported {
			next = watchRetry(delay, auth)
		}
		if !auth && !unsupported {
			delay = next
		}
		if !waitDecisionRetry(ctx, next) {
			return
		}
	}
}
func (r *runner) watchRemoteConnection(ctx context.Context, p machine.Profile, owner decisionMachine, lines chan<- decisionWatchLine) remoteWatchResult {
	child, stop := context.WithCancel(ctx)
	defer stop()
	cmd := childenv.CommandContext(child, "ssh", machine.ForwardArgs(p, r.remoteDecisionBinary(), []string{"decision", "watch"})...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return remoteWatchResult{reason: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return remoteWatchResult{reason: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return remoteWatchResult{reason: err.Error()}
	}
	var mu sync.Mutex
	reason := ""
	auth := false
	unsupported, observed := false, false
	var readers sync.WaitGroup
	note := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if value, ok := decisionAuthReason(line); ok {
			reason, auth = value, true
			stop()
		} else if decisionUnsupported(line) {
			reason, unsupported = "decision unsupported (未対応): "+strings.TrimSpace(line), true
		} else if !auth && !unsupported {
			reason = strings.TrimSpace(line)
		}
	}
	readers.Add(1)
	go func() {
		defer readers.Done()
		rd := bufio.NewReader(stderr)
		for {
			line, err := rd.ReadString('\n')
			if line != "" {
				note(line)
			}
			if err != nil {
				return
			}
		}
	}()
	rd := bufio.NewReader(stdout)
	for {
		raw, err := rd.ReadBytes('\n')
		if err != nil {
			if err != io.EOF && child.Err() == nil {
				note(err.Error())
			}
			break
		}
		if _, ok := decisionAuthReason(string(raw)); ok {
			note(string(raw))
			break
		}
		var line decisionWatchLine
		if err := json.Unmarshal(raw, &line); err != nil {
			if decisionUnsupported(string(raw)) {
				note(string(raw))
			} else {
				note("invalid remote decision stream: " + err.Error())
			}
			break
		}
		switch line.Event {
		case "machine_up", "machine_down":
		case "created", "resolved":
			if line.Decision == nil {
				note("remote decision stream missing record")
				stop()
				continue
			}
		default:
			note("unsupported remote decision event: " + line.Event)
			stop()
			continue
		}
		mu.Lock()
		observed = true
		if unsupported {
			unsupported = false
			reason = ""
		}
		mu.Unlock()
		line.Machine = owner
		if !sendWatch(ctx, lines, line) {
			break
		}
	}
	stop()
	readers.Wait()
	err = cmd.Wait()
	mu.Lock()
	defer mu.Unlock()
	if reason == "" {
		if err != nil {
			reason = err.Error()
		} else {
			reason = "SSH stream closed"
		}
	}
	return remoteWatchResult{reason: reason, auth: auth, unsupported: unsupported, observed: observed}
}

const decisionUnsupportedRetry = 30 * time.Minute // SPEC decision watch, 工程3A.
type remoteWatchResult struct {
	reason                      string
	auth, unsupported, observed bool
}

func decisionUnsupported(line string) bool {
	text := strings.ToLower(line)
	return strings.Contains(text, "decision") && (strings.Contains(text, "unknown command") || strings.Contains(text, "unknown subcommand") || strings.Contains(text, "unrecognized subcommand") || strings.Contains(text, "unrecognised subcommand"))
}
func remoteWatchRetry(unsupported *bool, delay time.Duration, result remoteWatchResult) (bool, time.Duration) {
	if result.observed {
		*unsupported = false
	}
	if result.unsupported {
		notify := !*unsupported
		*unsupported = true
		return notify, decisionUnsupportedRetry
	}
	if *unsupported {
		return false, decisionUnsupportedRetry
	}
	return true, watchRetry(delay, result.auth)
}
