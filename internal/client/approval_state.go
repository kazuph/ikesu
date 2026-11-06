package client

import (
	"sort"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
)

const approvalTypingWindow = 10 * time.Second

// approvalState belongs to a display connection, never to the server decision.
type approvalState struct {
	boot      string
	revision  uint64
	pending   []api.Decision
	seen      map[string]bool
	dismissed map[string]bool
	pane      string
	open      string
	selected  string
	checked   map[string]bool
	textMode  bool
	reason    string
	scroll    int
	waiting   bool
	painted   string
}

func (s *approvalState) update(p *endpoint.DecisionProjection, focused string, now, lastInput time.Time) {
	if p == nil || (s.boot == p.BootID && p.Revision < s.revision) {
		return
	}
	if s.seen == nil {
		s.seen = make(map[string]bool)
		s.dismissed = make(map[string]bool)
	}
	s.boot, s.revision = p.BootID, p.Revision
	s.pending = nil
	for _, d := range p.Pending {
		if (d.Kind != "command_guard" && !(d.Kind == "agent_prompt" && d.LateAnswer) && !(d.Kind == "tool_permission" && d.Origin != nil && d.Origin.Tool != "" && d.Origin.Agent == "claude")) || d.Status != "pending" || d.Origin == nil || d.Origin.OutsidePane || (d.Origin.Machine != "" && d.Origin.Machine != "local") {
			continue
		}
		if _, ok := approvalPaneID(d.Origin.PaneID); ok {
			s.pending = append(s.pending, d)
		}
	}
	sort.SliceStable(s.pending, func(i, j int) bool { return s.pending[i].CreatedUnixMS < s.pending[j].CreatedUnixMS })
	oldOpen := s.open
	exists := false
	for _, d := range s.pending {
		if d.DecisionID == oldOpen {
			exists = true
			break
		}
	}
	if oldOpen != "" && !exists {
		s.open, s.selected, s.reason, s.scroll, s.waiting = "", "", "", 0, false
		s.checked, s.textMode = nil, false
		s.painted = ""
	}
	for _, d := range s.pending {
		if !s.seen[d.DecisionID] || oldOpen != "" && !exists {
			s.seen[d.DecisionID] = true
			if d.Kind == "command_guard" && s.open == "" && d.Origin.PaneID == focused && (lastInput.IsZero() || now.Sub(lastInput) >= approvalTypingWindow) && !s.dismissed[d.DecisionID] {
				s.open = d.DecisionID
			}
		}
	}
	s.pane = focused
}

func (s *approvalState) dismiss() {
	if s.open != "" {
		s.dismissed[s.open] = true
	}
	s.open, s.selected, s.reason, s.scroll, s.waiting = "", "", "", 0, false
	s.checked, s.textMode = nil, false
	s.painted = ""
}
func (s *approvalState) openPane(pane string) {
	for _, d := range s.pending {
		if d.Origin.PaneID == pane {
			s.painted = ""
			s.checked, s.textMode = nil, false
			s.open, s.selected, s.reason, s.scroll, s.waiting = d.DecisionID, "", "", 0, false
			return
		}
	}
}
func (s *approvalState) focus(pane string) {
	if s.pane == pane {
		return
	}
	s.open, s.selected, s.reason, s.scroll, s.waiting = "", "", "", 0, false
	s.checked, s.textMode = nil, false
	s.painted = ""
	s.pane = pane
	for _, d := range s.pending {
		if d.Kind == "command_guard" && d.Origin.PaneID == pane && !s.dismissed[d.DecisionID] {
			s.open = d.DecisionID
			return
		}
	}
}

func (s *approvalState) current() *api.Decision {
	for i := range s.pending {
		if s.pending[i].DecisionID == s.open {
			return &s.pending[i]
		}
	}
	return nil
}

func (s *approvalState) selectOption(number int) {
	d := s.current()
	if d != nil && number > 0 && number <= len(d.Options) {
		s.selected = d.Options[number-1].ID
	}
}

func (s *approvalState) selectedReject() bool {
	d := s.current()
	if d == nil {
		return false
	}
	for _, option := range d.Options {
		if option.ID == s.selected {
			return option.Role == "reject"
		}
	}
	return false
}

func (s *approvalState) answer() *api.DecisionAnswerParams {
	d := s.current()
	if d == nil || s.waiting {
		return nil
	}
	if d.LateAnswer {
		answer := &api.DecisionAnswerParams{DecisionID: d.DecisionID}
		for _, option := range d.Options {
			if d.MultiSelect && s.checked[option.ID] || !d.MultiSelect && s.selected == option.ID {
				answer.OptionIDs = append(answer.OptionIDs, option.ID)
			}
		}
		if d.AllowText && s.reason != "" {
			text := s.reason
			answer.Text = &text
		}
		if len(answer.OptionIDs) == 0 && answer.Text == nil {
			return nil
		}
		return answer
	}
	if s.selected == "" {
		return nil
	}
	for _, option := range d.Options {
		if option.ID == s.selected {
			id := s.selected
			answer := &api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &id}
			if d.AllowText && s.reason != "" {
				text := s.reason
				answer.Text = &text
			}
			return answer
		}
	}
	return nil
}
