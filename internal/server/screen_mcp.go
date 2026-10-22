package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
)

func (s *Server) deliverMCPAnswer(pid int, signature string, prompt *screenprompt.Prompt, answer api.DecisionAnswer) error {
	if answer.Text != nil || answer.OptionID == nil {
		return fmt.Errorf("MCP confirmation requires one option without free text")
	}
	target := -1
	for index, id := range prompt.Items {
		if id == *answer.OptionID {
			target = index
		}
	}
	if target < 0 {
		return fmt.Errorf("MCP confirmation option unavailable")
	}
	var current *screenprompt.Prompt
	read := func() { s.app.Do(func() { current = s.readScreenPrompt(pid) }) }
	read()
	if current == nil || current.Signature() != signature || current.MCPServer != prompt.MCPServer || current.Cursor < 0 {
		return fmt.Errorf("MCP question or server changed")
	}
	if current.Cursor != target {
		arrow := "\x1b[B"
		if target < current.Cursor {
			arrow = "\x1b[A"
		}
		if err := s.screenInput(pid, signature, arrow, true, nil); err != nil {
			return err
		}
		if err := s.screenInputDelay(); err != nil {
			return err
		}
		read()
	}
	if current == nil || current.Signature() != signature || current.MCPServer != prompt.MCPServer || current.Cursor != target {
		return fmt.Errorf("MCP selection did not match after navigation")
	}
	return s.screenInputChecked(pid, signature, "\r", true, nil, &target)
}

func (s *Server) confirmPreviewAnswer(pid int, signature, option string) error {
	if err := s.screenInputDelay(); err != nil {
		return err
	}
	var current *screenprompt.Prompt
	s.app.Do(func() { current = s.readScreenPrompt(pid) })
	if current == nil || current.Signature() != signature || !current.HasPreview || current.Cursor < 0 || current.Cursor >= len(current.Items) || current.Items[current.Cursor] != option {
		return fmt.Errorf("preview question or selected option changed")
	}
	return s.screenInputChecked(pid, signature, "\r", true, nil, &current.Cursor)
}
