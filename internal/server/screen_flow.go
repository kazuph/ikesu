package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"os"
	"strings"
)

type screenAnswerFlow struct {
	name          string
	total         int
	inputSequence uint64
	answered      map[int]bool
	submitted     bool
	delivering    bool
}

func (s *Server) beginScreenFlowAnswer(pid int, p *screenprompt.Prompt) {
	pane := s.sup.Get(pid)
	if pane == nil {
		return
	}
	w := s.screenPrompts.watches[pid]
	w.answerInputSequence = pane.ExternalInputSequence()
	if p.Flow == "" || p.Question < 0 {
		return
	}
	if s.screenPrompts.flows == nil {
		s.screenPrompts.flows = map[int]*screenAnswerFlow{}
	}
	f := s.screenPrompts.flows[pid]
	if f == nil || f.name != p.Flow || f.submitted {
		f = &screenAnswerFlow{name: p.Flow, total: p.Total, inputSequence: pane.ExternalInputSequence(), answered: map[int]bool{}}
		s.screenPrompts.flows[pid] = f
	}
	f.delivering = true
}
func (s *Server) screenAnswerInterrupted(pid int) bool {
	pane := s.sup.Get(pid)
	w := s.screenPrompts.watches[pid]
	return pane == nil || w == nil || pane.ExternalInputSequence() != w.answerInputSequence
}
func (s *Server) finishScreenFlowAnswer(pid int, p *screenprompt.Prompt, err error) {
	f := s.screenPrompts.flows[pid]
	if f == nil || f.name != p.Flow {
		return
	}
	f.delivering = false
	if err == nil && !s.screenAnswerInterrupted(pid) {
		f.answered[p.Question] = true
	}
}
func (s *Server) submitScreenFlow(pid int, p *screenprompt.Prompt) {
	f := s.screenPrompts.flows[pid]
	pane := s.sup.Get(pid)
	if f == nil || f.submitted || f.delivering || f.total == 0 || pane == nil || s.screenUserActive(pid) || pane.ExternalInputSequence() != f.inputSequence {
		return
	}
	if p.Flow != "" && p.Flow != f.name {
		return
	}
	for i := 0; i < f.total; i++ {
		if !f.answered[i] {
			return
		}
	}
	for _, o := range p.Params.Options {
		if !strings.HasPrefix(strings.ToLower(o.Label), "submit") {
			continue
		}
		key := p.Keys[o.ID]
		if p.Cursor >= 0 && p.Cursor < len(p.Items) && p.Items[p.Cursor] == o.ID {
			key = "\r"
		}
		n, err := pane.WriteScreenPrompt([]byte(key), &f.inputSequence)
		if err != nil || n != len(key) {
			message := fmt.Sprintf("screen prompt Submit failed: written=%d: %v", n, err)
			fmt.Fprintln(os.Stderr, "ikesu: "+message)
			s.app.SetConfigWarning(message)
			return
		}
		f.submitted = true
		return
	}
}
