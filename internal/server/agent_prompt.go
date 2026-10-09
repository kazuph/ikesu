package server

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/runtime"
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

func (s *Server) agentPrompt(req *api.Request) api.Response {
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
	var pane *runtime.Pane
	var info api.AgentInfo
	var failure *api.Error
	s.app.Do(func() {
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
	})
	if failure != nil {
		return api.Failure(req.ID, failure.Code, failure.Message)
	}
	if p.Wait != nil && p.Wait.TimeoutMS != nil && *p.Wait.TimeoutMS == 0 {
		return api.Failure(req.ID, api.ErrTimeout, "timed out waiting for agent status")
	}
	if pane == nil {
		return api.Failure(req.ID, "agent_prompt_failed", "PTY actor closed during input submission")
	}
	if info.Agent != nil && *info.Agent == "copilot" {
		if _, err := pane.WriteToPTY([]byte("\x1b[I")); err != nil {
			return api.Failure(req.ID, "agent_prompt_failed", err.Error())
		}
	}
	text := p.Text
	if pane.BracketedPasteMode() {
		text = "\x1b[200~" + text + "\x1b[201~"
	}
	if _, err := pane.WriteToPTY([]byte(text)); err != nil {
		return api.Failure(req.ID, "agent_prompt_failed", err.Error())
	}
	timer := time.NewTimer(agentPromptSubmitDelay)
	select {
	case <-timer.C:
	case <-pane.Exited():
		timer.Stop()
		return api.Failure(req.ID, "agent_prompt_failed", "PTY actor closed during input submission")
	case <-s.sup.Done():
		timer.Stop()
		return api.Failure(req.ID, "agent_prompt_failed", "PTY actor closed during input submission")
	}
	if pane.Snapshot().Dead || s.sup.Get(pane.ID) != pane {
		return api.Failure(req.ID, "agent_prompt_failed", "PTY actor closed during input submission")
	}
	if _, err := pane.WriteToPTY([]byte{'\r'}); err != nil {
		return api.Failure(req.ID, "agent_prompt_failed", err.Error())
	}
	if p.Wait == nil {
		s.app.Do(func() { _, info, failure = s.promptTarget(req.ID, p.Target) })
		if failure != nil {
			return api.Failure(req.ID, failure.Code, failure.Message)
		}
		return api.Success(req.ID, api.AgentPromptedResult{Type: "agent_prompted", Agent: info})
	}
	return s.waitPrompt(req.ID, p, requestStarted)
}

func (s *Server) waitPrompt(id string, p api.AgentPromptParams, requestStarted time.Time) api.Response {
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
		s.app.Do(func() { _, info, failure = s.promptTarget(id, p.Target) })
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
		}
	}
}
