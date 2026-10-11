package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/machine"
	"golang.org/x/sys/unix"
)

// Retry bounds reuse saved-endpoint supervision (SPEC G11 P4).
const decisionRetryInitial = 500 * time.Millisecond
const decisionRetryLimit = 120 * time.Second
const decisionRetryStable = 60 * time.Second
const decisionAuthRetry = 10 * time.Minute // SPEC G9 watch authentication delay.

type decisionMachine struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type decisionWatchLine struct {
	Event    string          `json:"event"`
	Machine  decisionMachine `json:"machine"`
	Decision *api.Decision   `json:"decision,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

func watchRetry(previous time.Duration, auth bool) time.Duration {
	if auth {
		return decisionAuthRetry
	}
	if previous == 0 {
		return decisionRetryInitial
	}
	return min(decisionRetryLimit, previous*2)
}
func waitDecisionRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func sendWatch(ctx context.Context, lines chan<- decisionWatchLine, line decisionWatchLine) bool {
	select {
	case lines <- line:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *runner) decisionWatch(args []string) int {
	all := false
	if len(args) == 1 && args[0] == "--all-machines" {
		all = true
	} else if len(args) != 0 {
		return usageErr(r, "usage: gtx decision watch [--all-machines]")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	stopPipe := watchOutputClosed(r.out(), cancel)
	defer stopPipe()
	lines := make(chan decisionWatchLine)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); r.watchLocal(ctx, lines) }()
	if all {
		for _, p := range r.catalog().Load() {
			if !p.Enabled {
				continue
			}
			workers.Add(1)
			go func(p machine.Profile) { defer workers.Done(); r.watchRemote(ctx, p, lines) }(p)
		}
	}
	defer workers.Wait()
	encoder := json.NewEncoder(r.out())
	for {
		select {
		case <-ctx.Done():
			return 0
		case line := <-lines:
			if err := encoder.Encode(line); err != nil {
				cancel()
				return 0
			}
			if f, ok := r.out().(interface{ Flush() error }); ok {
				if err := f.Flush(); err != nil {
					cancel()
					return 0
				}
			}
		}
	}
}

// Poll an owned descriptor without altering stdout or the user's terminal.
// A pipe consumer that exits while the stream is idle still ends the watch.
func watchOutputClosed(out io.Writer, cancel context.CancelFunc) func() {
	file, ok := out.(*os.File)
	if !ok {
		return func() {}
	}
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		return func() {}
	}
	unix.CloseOnExec(fd)
	wake, finish, err := os.Pipe()
	if err != nil {
		unix.Close(fd)
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if outputPipeClosed(fd, int(wake.Fd())) {
			cancel()
		}
	}()
	return func() { finish.Close(); <-done; wake.Close(); unix.Close(fd) }
}
func (r *runner) watchLocal(ctx context.Context, lines chan<- decisionWatchLine) {
	owner := decisionMachine{ID: "local", Label: "Local"}
	delay := time.Duration(0)
	for ctx.Err() == nil {
		started := time.Now()
		err := r.watchLocalConnection(ctx, owner, lines)
		if ctx.Err() != nil {
			return
		}
		reason := "server disconnected"
		if err != nil {
			reason = err.Error()
		}
		if !sendWatch(ctx, lines, decisionWatchLine{Event: "machine_down", Machine: owner, Reason: reason}) {
			return
		}
		if time.Since(started) >= decisionRetryStable {
			delay = 0
		}
		delay = watchRetry(delay, false)
		if !waitDecisionRetry(ctx, delay) {
			return
		}
	}
}
func (r *runner) watchLocalConnection(ctx context.Context, owner decisionMachine, lines chan<- decisionWatchLine) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", r.socketPath())
	if err != nil {
		return err
	}
	defer conn.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-finished:
		}
	}()
	if err := api.WriteRequest(conn, api.Request{ID: "cli:decision:watch", Method: api.MethodEventsSubscribe, Params: api.MarshalParams(api.EventsSubscribeParams{Subscriptions: []api.Subscription{{Type: "decision.created"}, {Type: "decision.resolved"}}})}); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	response, err := api.ReadResponse(reader)
	if err != nil {
		return err
	}
	if response.Err != nil {
		return response.Err
	}
	// Subscribe first, then snapshot all statuses. Replayed journal entries
	// already represented by that snapshot are discarded, without a time heuristic.
	raw, err := r.request("cli:decision:watch:list", api.MethodDecisionList, api.DecisionListParams{})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}
	if response.Err != nil {
		return response.Err
	}
	var list api.DecisionListResult
	if err := json.Unmarshal(response.Result, &list); err != nil {
		return err
	}
	seen := map[string]string{}
	if !sendWatch(ctx, lines, decisionWatchLine{Event: "machine_up", Machine: owner}) {
		return ctx.Err()
	}
	for i := range list.Decisions {
		d := list.Decisions[i]
		seen[d.DecisionID] = d.Status
		if d.Status == "pending" {
			if !sendWatch(ctx, lines, decisionWatchLine{Event: "created", Machine: owner, Decision: &d}) {
				return ctx.Err()
			}
		}
	}
	for {
		raw, err := reader.ReadBytes('\n')
		if err != nil {
			return err
		}
		var env api.EventEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return err
		}
		var d api.Decision
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		event := ""
		switch env.Event {
		case "decision_created":
			if _, known := seen[d.DecisionID]; known {
				continue
			}
			if d.Status != "pending" {
				return fmt.Errorf("created decision is not pending")
			}
			event = "created"
		case "decision_resolved":
			if seen[d.DecisionID] == d.Status {
				continue
			}
			switch d.Status {
			case "answered", "expired", "cancelled":
			default:
				return fmt.Errorf("resolved decision is still pending")
			}
			event = "resolved"
		default:
			continue
		}
		seen[d.DecisionID] = d.Status
		if !sendWatch(ctx, lines, decisionWatchLine{Event: event, Machine: owner, Decision: &d}) {
			return ctx.Err()
		}
	}
}
