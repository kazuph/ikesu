package vt

import (
	"bytes"
	"encoding/gob"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"image/color"
)

func init() {
	gob.Register(color.RGBA{})
	gob.Register(color.NRGBA{})
	gob.Register(color.RGBA64{})
	gob.Register(color.NRGBA64{})
	gob.Register(color.Gray{})
	gob.Register(color.Gray16{})
	gob.Register(ansi.BasicColor(0))
	gob.Register(ansi.IndexedColor(0))
	gob.Register(ansi.TrueColor(0))
	gob.Register(ansi.RGBColor{})
	gob.Register(ansi.DECMode(0))
	gob.Register(ansi.ANSIMode(0))
}

type screenTransfer struct {
	Width, Height           int
	Cells                   []uv.Line
	History                 []uv.Line
	HistoryLimit, ByteLimit int
	ByteLimited             bool
	Cursor, Saved           Cursor
	Scroll                  uv.Rectangle
}
type terminalTransfer struct {
	Screens                                       [2]screenTransfer
	Alternate                                     bool
	Modes                                         ansi.Modes
	Colors                                        [256]color.Color
	DefaultFg, DefaultBg, DefaultCur, Fg, Bg, Cur color.Color
	Charsets                                      [4]CharSet
	GL, GR, GSingle, Keyboard                     int
	KeyboardState                                 *keyboardTransfer
	Last                                          rune
	Grapheme                                      []byte
	Phantom                                       bool
	Icon, Title, Cwd                              string
	Tabs                                          []int
	Partial                                       []byte
}

// MarshalHandoff captures terminal state rather than replaying terminal output.
// Incomplete sequences are restored into the parser without replaying effects.
func (e *Emulator) MarshalHandoff() ([]byte, error) {
	t := terminalTransfer{Alternate: e.scr == &e.scrs[1], Modes: e.modes, Colors: e.colors,
		DefaultFg: e.defaultFg, DefaultBg: e.defaultBg, DefaultCur: e.defaultCur, Fg: e.fgColor, Bg: e.bgColor, Cur: e.curColor,
		Charsets: e.charsets, GL: e.gl, GR: e.gr, GSingle: e.gsingle, Keyboard: len(e.keyboard.Stack), KeyboardState: &e.keyboard, Last: e.lastChar, Grapheme: e.grapheme,
		Phantom: e.atPhantom, Icon: e.iconName, Title: e.title, Cwd: e.cwd, Partial: e.partial}
	for x := 0; x < e.tabstops.Width(); x++ {
		if e.tabstops.IsStop(x) {
			t.Tabs = append(t.Tabs, x)
		}
	}
	for i := range e.scrs {
		s := &e.scrs[i]
		v := screenTransfer{Width: s.Width(), Height: s.Height(), Cursor: s.cur, Saved: s.saved, Scroll: s.scroll, HistoryLimit: s.scrollback.maxLines, ByteLimited: s.scrollback.byteLimited, ByteLimit: s.scrollback.maxBytes}
		for y := 0; y < s.Height(); y++ {
			v.Cells = append(v.Cells, append(uv.Line(nil), s.buf.Line(y)...))
		}
		for _, line := range s.scrollback.Lines() {
			v.History = append(v.History, append(uv.Line(nil), line...))
		}
		t.Screens[i] = v
	}
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(t)
	return b.Bytes(), err
}

func (e *Emulator) UnmarshalHandoff(data []byte) error {
	var t terminalTransfer
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&t); err != nil {
		return err
	}
	for i, v := range t.Screens {
		if v.Width <= 0 || v.Height <= 0 || len(v.Cells) != v.Height {
			return fmt.Errorf("invalid terminal handoff dimensions")
		}
		s := NewScreen(v.Width, v.Height)
		for y, line := range v.Cells {
			if len(line) != v.Width {
				return fmt.Errorf("invalid terminal handoff row")
			}
			copy(s.buf.Line(y), line)
		}
		s.cur, s.saved, s.scroll = v.Cursor, v.Saved, v.Scroll
		s.scrollback = NewScrollback(v.HistoryLimit)
		if v.ByteLimited {
			s.scrollback.SetMaxBytes(v.ByteLimit)
		}
		for _, line := range v.History {
			s.scrollback.Push(line)
		}
		e.scrs[i] = *s
		e.scrs[i].cb = &e.cb
	}
	e.scr = &e.scrs[0]
	if t.Alternate {
		e.scr = &e.scrs[1]
	}
	e.modes, e.colors = t.Modes, t.Colors
	e.defaultFg, e.defaultBg, e.defaultCur = t.DefaultFg, t.DefaultBg, t.DefaultCur
	e.fgColor, e.bgColor, e.curColor = t.Fg, t.Bg, t.Cur
	e.charsets, e.gl, e.gr, e.gsingle = t.Charsets, t.GL, t.GR, t.GSingle
	e.lastChar, e.grapheme, e.atPhantom = t.Last, t.Grapheme, t.Phantom
	e.keyboard = keyboardTransfer{}
	if t.KeyboardState != nil {
		e.keyboard = *t.KeyboardState
	} else if t.Keyboard > 0 {
		// Older servers only knew the push depth; retain their explicit/raw behavior.
		e.keyboard.Legacy = true
		e.keyboard.Stack = make([]keyboardEntry, t.Keyboard)
		for i := range e.keyboard.Stack {
			e.keyboard.Stack[i].Legacy = i > 0
		}
	}
	e.iconName, e.title, e.cwd = t.Icon, t.Title, t.Cwd
	e.tabstops = uv.DefaultTabStops(e.Width())
	for x := 0; x < e.Width(); x++ {
		e.tabstops.Reset(x)
	}
	for _, x := range t.Tabs {
		e.tabstops.Set(x)
	}
	e.parser.Reset()
	e.parser.SetHandler(ansi.Handler{})
	e.parser.Parse(t.Partial)
	e.partial = t.Partial
	e.setParserHandler()
	return nil
}

func (e *Emulator) setParserHandler() {
	e.parser.SetHandler(ansi.Handler{Print: e.handlePrint, Execute: e.handleControl,
		HandleCsi: e.handleCsi, HandleEsc: e.handleEsc, HandleDcs: e.handleDcs,
		HandleOsc: e.handleOsc, HandleApc: e.handleApc, HandlePm: e.handlePm, HandleSos: e.handleSos})
}
