package server

import (
	"fmt"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
)

func (s *Server) deliverCodexQuestionAnswer(pid int, signature string, prompt *screenprompt.Prompt, answer api.DecisionAnswer) error {
	if answer.Text == nil {
		if answer.OptionID == nil || prompt.TextOpen {
			return fmt.Errorf("cannot choose an option while Codex notes are open")
		}
		return s.screenInput(pid, signature, prompt.Keys[*answer.OptionID], true, nil)
	}
	if !prompt.TextOpen {
		if err := s.navigateScreenRow(pid, signature, prompt.TextID); err != nil {
			return err
		}
		if err := s.screenInput(pid, signature, "\t", true, nil); err != nil {
			return err
		}
		if err := s.screenInputDelay(); err != nil {
			return err
		}
	}
	open := true
	for _, data := range []string{*answer.Text, "\r"} {
		if err := s.screenInput(pid, signature, data, true, &open); err != nil {
			return err
		}
		if err := s.screenInputDelay(); err != nil {
			return err
		}
	}
	return nil
}
