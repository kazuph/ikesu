package client

import (
	"fmt"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
)

func (m *machineClient) approvalInputEvent(event input.Event) (bool, error) {
	e := m.endpoints[m.active]
	if e == nil || e.snapshot == nil || e.snapshot.State == nil {
		return false, nil
	}
	state := &e.guardApproval
	if state.current() != nil {
		switch event.Kind {
		case input.KindPaste:
			if state.selected == "reject" && !state.waiting {
				state.reason += event.Text
				return true, m.paint()
			}
			return true, nil
		case input.KindMouseRelease:
			m.suppressRelease = false
			return true, nil
		case input.KindKeyRelease, input.KindMouseClick, input.KindMouseWheel, input.KindMouseMotion:
			return true, nil
		case input.KindKey:
			key := event.Key
			switch {
			case key.Code == uv.KeyEscape && key.Mod == 0:
				state.dismiss()
			case state.waiting:
				return true, nil
			case key.Code >= '1' && key.Code <= '9' && key.Mod == 0:
				state.selectOption(int(key.Code - '0'))
			case key.Code == uv.KeyEnter && key.Mod == 0:
				if answer := state.answer(); answer != nil && e.connection != nil {
					if err := e.connection.enqueue(msg{Type: "decision_answer", DecisionAnswer: answer}); err != nil {
						return true, err
					}
					state.waiting = true
				}
			case key.Code == uv.KeyUp:
				state.scroll = max(0, state.scroll-1)
			case key.Code == uv.KeyDown:
				state.scroll++
			case state.selected == "reject" && key.Code == uv.KeyBackspace:
				_, n := utf8.DecodeLastRuneInString(state.reason)
				if n > 0 {
					state.reason = state.reason[:len(state.reason)-n]
				}
			case state.selected == "reject" && key.Mod == 0 && key.Text != "":
				state.reason += key.Text
			}
			return true, m.paint()
		}
		return false, nil
	}
	if event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseLeft && m.regions != nil {
		for pane, r := range m.regions.AnswerRects {
			if contains(r.X, r.Y, r.W, r.H, event.Mouse.X, event.Mouse.Y) {
				state.openPane(fmt.Sprintf("p%d", pane))
				m.suppressRelease = true
				return true, m.paint()
			}
		}
	}
	if event.Kind != input.KindKey {
		return false, nil
	}
	cfg := e.snapshot.SidebarConfig
	prefix := e.approvalPrefix || e.snapshot.Prefix
	e.approvalPrefix = false
	for _, spec := range cfg.ActionBindings("open_guard_approval") {
		chord, err := config.ParseChord(spec)
		if err != nil || !chord.Match(event.Key, prefix) {
			continue
		}
		// Custom commands retain precedence over the default binding.
		for _, binding := range cfg.Keys.Command {
			other, err := config.ParseChord(binding.Key)
			if err == nil && other.Match(event.Key, prefix) {
				return false, nil
			}
		}
		if prefix && e.connection != nil {
			if err := e.connection.enqueue(msg{Type: "input", EndPrefix: true}); err != nil {
				return true, err
			}
		}
		state.openPane(fmt.Sprintf("p%d", e.snapshot.State.FocusedPane()))
		return true, m.paint()
	}
	prefixSpec := cfg.Keys.Prefix
	if prefixSpec == "" {
		prefixSpec = "ctrl+b"
	}
	if chord, err := config.ParseChord(prefixSpec); err == nil && chord.Match(event.Key, false) {
		e.approvalPrefix = true
	}
	return false, nil
}
