package server

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/runtime"
)

// Isolated distribution observations are saved in herdr-prompt-*.json.
const agentPromptSubmitDelay = 300 * time.Millisecond
const agentPromptStartDeadline = 5 * time.Second

func foregroundAgent(p *runtime.Pane) string {
	job := p.ForegroundJob()
	dj := detect.Job{ProcessGroupID: job.ProcessGroupID}
	for _, pr := range job.Processes {
		dj.Processes = append(dj.Processes, detect.JobProcess{PID: pr.PID, Name: pr.Name, Argv: pr.Argv, Cmdline: pr.Cmdline})
	}
	id, _ := detect.IdentifyJob(dj)
	return id
}

func (s *Server) promptTarget(id, target string) (*runtime.Pane, api.AgentInfo, *api.Error) {
	t, err := s.resolveTarget(target, true)
	if err != nil {
		return nil, api.AgentInfo{}, err
	}
	pane := s.sup.Get(t.pid)
	if pane == nil || pane.Snapshot().Dead {
		return nil, api.AgentInfo{}, &api.Error{Code: "agent_prompt_failed", Message: "PTY actor closed during input submission"}
	}
	return pane, s.agentInfoAt(t.pid, t.wsIdx, t.tabIdx), nil
}

func (s *Server) agentPrompt(ctx context.Context, req *api.Request) api.Response {
	requestStarted := time.Now()
	var p api.AgentPromptParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if strings.TrimSpace(p.Text) == "" {
		return api.Failure(req.ID, "empty_agent_prompt", "agent prompt must not be empty")
	}
	if p.Wait != nil {
		if p.Wait.TimeoutMS != nil && *p.Wait.TimeoutMS > math.MaxInt64/uint64(time.Millisecond) {
			return api.Failure(req.ID, api.ErrInvalidRequest, "timeout_ms is too large")
		}
		for _, status := range p.Wait.Until {
			switch status {
			case api.AgentIdle, api.AgentWorking, api.AgentBlocked, api.AgentDone, api.AgentUnknown:
			default:
				return api.Failure(req.ID, api.ErrInvalidRequest, "invalid until status: "+status)
			}
		}
	}
	if p.Wait != nil && p.Wait.TimeoutMS != nil && *p.Wait.TimeoutMS == 0 {
		return api.Failure(req.ID, api.ErrTimeout, "timed out waiting for agent status")
	}
	var pane *runtime.Pane
	var op *runtime.InputOperation
	var manualSequence uint64
	var info api.AgentInfo
	var failure *api.Error
	if !s.app.DoContext(ctx, func() {
		if s.inputHandoffFrozen {
			failure = &api.Error{Code: "handoff_in_progress", Message: "pane input is unavailable during handoff"}
			return
		}
		pane, info, failure = s.promptTarget(req.ID, p.Target)
		if failure != nil {
			return
		}
		switch {
		case info.AgentStatus == api.AgentBlocked:
			failure = &api.Error{Code: "agent_blocked", Message: fmt.Sprintf("agent %s is blocked and requires interactive input", info.PaneID)}
		case info.LaunchPending:
			failure = &api.Error{Code: "agent_not_ready", Message: fmt.Sprintf("agent %s is not an active named agent", info.PaneID)}
		case info.AgentStatus == api.AgentUnknown:
			failure = &api.Error{Code: "agent_not_ready", Message: "agent status is unknown"}
		case foregroundAgent(pane) == "":
			failure = &api.Error{Code: "agent_not_ready", Message: fmt.Sprintf("agent %s is no longer the pane foreground process", info.PaneID)}
		case !info.InteractiveRdy:
			failure = &api.Error{Code: "agent_not_ready", Message: "agent is not ready"}
		}
		if failure == nil {
			manualSequence = pane.ManualInputSequence()
			if _, ok := pane.RecordKeyInputAndDo(manualSequence, nil); !ok {
				failure = &api.Error{Code: "agent_prompt_failed", Message: "pane input interrupted submission"}
			} else {
				op = pane.ReserveInputOperation()
			}
		}
	}) {
		return api.Failure(req.ID, "agent_prompt_failed", "request canceled before execution")
	}
	if failure != nil {
		return api.Failure(req.ID, failure.Code, failure.Message)
	}
	if pane == nil {
		return api.Failure(req.ID, "agent_prompt_failed", "PTY actor closed during input submission")
	}
	if err := s.submitAgentPrompt(ctx, pane, op, manualSequence, p.Text, info.Agent != nil && *info.Agent == "copilot"); err != nil {
		return api.Failure(req.ID, "agent_prompt_failed", err.Error())
	}
	if p.Wait == nil {
		if !s.app.DoContext(ctx, func() { _, info, failure = s.promptTarget(req.ID, p.Target) }) {
			return api.Failure(req.ID, "agent_prompt_failed", "request canceled after input")
		}
		if failure != nil {
			return api.Failure(req.ID, failure.Code, failure.Message)
		}
		return api.Success(req.ID, api.AgentPromptedResult{Type: "agent_prompted", Agent: info})
	}
	return s.waitPrompt(ctx, req.ID, p, requestStarted)
}

func (s *Server) submitAgentPrompt(ctx context.Context, pane *runtime.Pane, op *runtime.InputOperation, manualSequence uint64, text string, copilot bool) error {
	if !op.Wait(ctx) {
		return fmt.Errorf("request canceled before input")
	}
	if ctx.Err() != nil || pane.ManualInputSequence() != manualSequence {
		op.Release()
		return fmt.Errorf("request canceled before input")
	}
	if copilot {
		if _, err := pane.WriteToPTY([]byte("\x1b[I")); err != nil {
			op.Release()
			return err
		}
	}
	if pane.BracketedPasteMode() {
		text = "\x1b[200~" + text + "\x1b[201~"
	}
	written, err := pane.QueueExternalInput(ctx, manualSequence, []byte(text))
	if err != nil {
		op.Release()
		return err
	}
	completed := make(chan error, 1)
	go func() {
		defer op.Release()
		completed <- s.finishAgentPrompt(pane, manualSequence, written)
	}()
	select {
	case err := <-completed:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) finishAgentPrompt(pane *runtime.Pane, manualSequence uint64, written <-chan error) error {
	if err := <-written; err != nil {
		return err
	}
	timer := time.NewTimer(agentPromptSubmitDelay)
	select {
	case <-timer.C:
	case <-pane.Exited():
		timer.Stop()
		return fmt.Errorf("PTY actor closed during input submission")
	case <-s.sup.Done():
		timer.Stop()
		return fmt.Errorf("PTY actor closed during input submission")
	}
	if pane.Snapshot().Dead || s.sup.Get(pane.ID) != pane {
		return fmt.Errorf("PTY actor closed during input submission")
	}
	_, err := pane.WritePTYIfInputUnchanged(manualSequence, []byte{'\r'})
	return err
}

func (s *Server) waitPrompt(ctx context.Context, id string, p api.AgentPromptParams, requestStarted time.Time) api.Response {
	started := time.Now()
	var deadline time.Time
	if p.Wait.TimeoutMS != nil {
		deadline = requestStarted.Add(time.Duration(*p.Wait.TimeoutMS) * time.Millisecond)
	}
	observed := false
	until := p.Wait.Until
	if len(until) == 0 {
		until = []api.AgentStatus{api.AgentIdle, api.AgentBlocked, api.AgentDone}
	}
	tick := time.NewTicker(eventWaitPoll)
	defer tick.Stop()
	for {
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return api.Failure(id, api.ErrTimeout, "timed out waiting for agent status")
		}
		var info api.AgentInfo
		var failure *api.Error
		if !s.app.DoContext(ctx, func() { _, info, failure = s.promptTarget(id, p.Target) }) {
			return api.Failure(id, "agent_prompt_failed", "request canceled while waiting")
		}
		if failure != nil {
			return api.Failure(id, failure.Code, failure.Message)
		}
		if info.PaneID == "" {
			return api.Failure(id, "agent_prompt_failed", "PTY actor closed during input submission")
		}
		if info.AgentStatus == api.AgentWorking || info.AgentStatus == api.AgentBlocked {
			observed = true
		}
		if observed {
			for _, status := range until {
				if info.AgentStatus == status {
					return api.Success(id, api.AgentPromptedResult{Type: "agent_prompted", Agent: info})
				}
			}
		}
		if !observed && time.Since(started) >= agentPromptStartDeadline {
			return api.Failure(id, "agent_prompt_stalled", fmt.Sprintf("agent prompt produced no observed working or blocked state within 5000 ms; current status is %s", info.AgentStatus))
		}
		select {
		case <-tick.C:
		case <-s.sup.Done():
			return api.Failure(id, "agent_prompt_failed", "PTY actor closed during input submission")
		case <-ctx.Done():
			return api.Failure(id, "agent_prompt_failed", "request canceled while waiting")
		}
	}
}
