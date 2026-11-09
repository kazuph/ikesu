package runtime

import (
	"errors"
	"image/color"
	"strings"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
)

// MailInputQuiet is the agreed period after external input during which mail waits.
const MailInputQuiet = 10 * time.Second

var ErrMailInputActive = errors.New("mail delivery deferred: pane input is active")

// MailInputActive reports recent external input or visible unsent agent input.
func (p *Pane) MailInputActive() bool {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	return p.mailInputActiveLocked()
}

func (p *Pane) mailInputActiveLocked() bool {
	last := p.lastMailExternalInput.Load()
	if last != 0 && time.Since(time.Unix(0, last)) < MailInputQuiet {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.visibleAgentInputLocked()
}

// HasUnsentAgentInput reports the visible text at an agent prompt.
func (p *Pane) UnsentAgentInput() (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agentInputLocked()
}

// WriteMail checks and writes under the same input admission lock.
// The bytes are accepted in order under the lock; nothing waits for the
// application to read them.
func (p *Pane) WriteMail(data []byte) (int, error) {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	if p.mailInputActiveLocked() {
		return 0, ErrMailInputActive
	}
	p.lastExternalInput.Store(time.Now().UnixNano())
	p.externalInputSeq.Add(1)
	return p.WriteToPTY(data)
}

func (p *Pane) visibleAgentInputLocked() bool {
	pending, _ := p.agentInputLocked()
	return pending
}

func (p *Pane) agentInputLocked() (bool, bool) {
	agent := p.agentID
	if p.agentOverride != "" {
		agent = p.agentOverride
	}
	marker := ""
	switch agent {
	case "codex":
		marker = "›"
	case "claude":
		marker = "❯"
	default:
		return false, false
	}
	height, width := p.emu.Height(), p.emu.Width()
	row, col := -1, -1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := p.emu.CellAt(x, y)
			if c == nil || strings.TrimFunc(c.Content, unicode.IsSpace) == "" {
				continue
			}
			if c.Content == marker {
				row, col = y, x
			}
			break
		}
	}
	if row < 0 {
		return false, false
	}
	background := p.emu.CellAt(col, row).Style.Bg
	var nonFaint, faint, reverse int
	for y := row; y < height; y++ {
		start := 0
		if y == row {
			start = col + 1
		}
		var line strings.Builder
		for x := start; x < width; x++ {
			if c := p.emu.CellAt(x, y); c != nil {
				line.WriteString(c.Content)
			}
		}
		if y > row {
			trimmed := strings.TrimSpace(line.String())
			if strings.HasPrefix(trimmed, "───") {
				break
			}
			if agent == "codex" && background != nil && trimmed != "" {
				var first *uv.Cell
				for x := start; x < width; x++ {
					c := p.emu.CellAt(x, y)
					if c != nil && strings.TrimFunc(c.Content, unicode.IsSpace) != "" {
						first = c
						break
					}
				}
				if first != nil && !sameMailBackground(background, first.Style.Bg) {
					break
				}
			} else if agent == "codex" && background == nil && trimmed == "" && y > p.cursor.Y {
				break
			}
		}
		for x := start; x < width; x++ {
			c := p.emu.CellAt(x, y)
			if c == nil || strings.TrimFunc(c.Content, unicode.IsSpace) == "" {
				continue
			}
			if c.Style.Attrs&uv.AttrFaint != 0 {
				faint++
			} else if c.Style.Attrs&uv.AttrReverse != 0 {
				reverse++
			} else {
				nonFaint++
			}
		}
	}
	// Claude renders the cursor over the first character of its faint placeholder.
	return nonFaint > 0 || faint == 0 && (reverse > 0 || p.cursor.Visible && (p.cursor.Y > row || p.cursor.Y == row && p.cursor.X > col+2)), true
}

func sameMailBackground(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
