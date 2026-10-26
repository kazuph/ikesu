//go:build flatcells

package vt

import (
	"image/color"
	"math"
	"reflect"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// FlatCell is the pointer-free screen representation. IDs belong to its screen.
// Width keeps the existing int domain instead of adding a terminal width limit.
type FlatCell struct {
	Text, Style, Link uint32
	Width             int
}

const flatIndirect uint32 = 1 << 31
const flatColorShift = 24

const (
	flatBasic uint32 = iota + 1
	flatIndexed
	flatTrueColor
	flatRGB
	flatRGBA
	flatNRGBA
)

type flatIDs[K comparable] struct {
	values []K
	refs   []int
	free   []uint32
	index  map[K]uint32
	live   int
}

func (p *flatIDs[K]) acquire(v K) uint32 {
	var zero K
	if v == zero {
		return 0
	}
	if id, ok := p.index[v]; ok {
		p.refs[id]++
		return id
	}
	if p.index == nil {
		p.index = make(map[K]uint32)
		p.values, p.refs = []K{zero}, []int{0}
	}
	var id uint32
	if n := len(p.free); n != 0 {
		id, p.free = p.free[n-1], p.free[:n-1]
	} else {
		if uint64(len(p.values)) > math.MaxUint32 {
			panic("flat screen: unsupported ID space exhausted")
		}
		id = uint32(len(p.values))
		p.values = append(p.values, zero)
		p.refs = append(p.refs, 0)
	}
	p.values[id], p.refs[id], p.index[v] = v, 1, id
	p.live++
	return id
}

func (p *flatIDs[K]) retain(id uint32) {
	if id != 0 {
		p.refs[id]++
	}
}

func (p *flatIDs[K]) release(id uint32) (K, bool) {
	var zero K
	if id == 0 {
		return zero, false
	}
	p.refs[id]--
	if p.refs[id] != 0 {
		return zero, false
	}
	v := p.values[id]
	delete(p.index, v)
	p.values[id] = zero
	p.free = append(p.free, id)
	p.live--
	if p.live == 0 {
		*p = flatIDs[K]{}
	}
	return v, true
}

type flatStyle struct {
	Fg, Bg, UnderlineColor uint32
	Underline              uv.Underline
	Attrs                  uint8
}

type flatColorSlot struct {
	value color.Color
	refs  int
}

type flatTables struct {
	text    flatIDs[string]
	styles  flatIDs[flatStyle]
	decoded []uv.Style
	links   flatIDs[uv.Link]
	colors  []flatColorSlot
	free    []uint32
	byColor map[color.Color]uint32
	live    int
}

func (t *flatTables) acquireColor(c color.Color) uint32 {
	var kind, rgb uint32
	switch v := c.(type) {
	case nil:
		return 0
	case ansi.BasicColor:
		return flatBasic<<flatColorShift | uint32(v)
	case ansi.IndexedColor:
		return flatIndexed<<flatColorShift | uint32(v)
	case ansi.TrueColor:
		if uint32(v) <= 0xffffff {
			return flatTrueColor<<flatColorShift | uint32(v)
		}
	case ansi.RGBColor:
		kind, rgb = flatRGB, uint32(v.R)<<16|uint32(v.G)<<8|uint32(v.B)
	case color.RGBA:
		if v.A == 255 {
			kind, rgb = flatRGBA, uint32(v.R)<<16|uint32(v.G)<<8|uint32(v.B)
		}
	case color.NRGBA:
		if v.A == 255 {
			kind, rgb = flatNRGBA, uint32(v.R)<<16|uint32(v.G)<<8|uint32(v.B)
		}
	}
	if kind != 0 {
		return kind<<flatColorShift | rgb
	}
	comparable := reflect.ValueOf(c).Comparable()
	if comparable {
		if id, ok := t.byColor[c]; ok {
			t.colors[id].refs++
			return flatIndirect | id
		}
	}
	if t.colors == nil {
		t.colors = []flatColorSlot{{}}
	}
	var id uint32
	if n := len(t.free); n != 0 {
		id, t.free = t.free[n-1], t.free[:n-1]
	} else {
		if uint64(len(t.colors)) >= uint64(flatIndirect) {
			panic("flat screen: unsupported color ID space exhausted")
		}
		id = uint32(len(t.colors))
		t.colors = append(t.colors, flatColorSlot{})
	}
	t.colors[id] = flatColorSlot{value: c, refs: 1}
	if comparable {
		if t.byColor == nil {
			t.byColor = make(map[color.Color]uint32)
		}
		t.byColor[c] = id
	}
	t.live++
	return flatIndirect | id
}

func (t *flatTables) releaseColor(code uint32) {
	if code&flatIndirect == 0 {
		return
	}
	id := code &^ flatIndirect
	s := &t.colors[id]
	s.refs--
	if s.refs != 0 {
		return
	}
	if reflect.ValueOf(s.value).Comparable() {
		delete(t.byColor, s.value)
	}
	*s = flatColorSlot{}
	t.free = append(t.free, id)
	t.live--
	if t.live == 0 {
		t.colors, t.free, t.byColor = nil, nil, nil
	}
}

func (t *flatTables) color(code uint32) color.Color {
	if code&flatIndirect != 0 {
		return t.colors[code&^flatIndirect].value
	}
	r, g, b := uint8(code>>16), uint8(code>>8), uint8(code)
	switch code >> flatColorShift {
	case flatBasic:
		return ansi.BasicColor(b)
	case flatIndexed:
		return ansi.IndexedColor(b)
	case flatTrueColor:
		return ansi.TrueColor(code & 0xffffff)
	case flatRGB:
		return ansi.RGBColor{R: r, G: g, B: b}
	case flatRGBA:
		return color.RGBA{R: r, G: g, B: b, A: 255}
	case flatNRGBA:
		return color.NRGBA{R: r, G: g, B: b, A: 255}
	default:
		return nil
	}
}

func (t *flatTables) pack(c uv.Cell) FlatCell {
	var text uint32
	if c.Content != "" {
		r, n := utf8.DecodeRuneInString(c.Content)
		if n == len(c.Content) && utf8.ValidString(c.Content) {
			text = uint32(r) + 1
		} else {
			id := t.text.acquire(c.Content)
			if id >= flatIndirect {
				panic("flat screen: unsupported grapheme ID space exhausted")
			}
			text = flatIndirect | id
		}
	}
	style := flatStyle{Fg: t.acquireColor(c.Style.Fg), Bg: t.acquireColor(c.Style.Bg), UnderlineColor: t.acquireColor(c.Style.UnderlineColor), Underline: c.Style.Underline, Attrs: c.Style.Attrs}
	_, existed := t.styles.index[style]
	id := t.styles.acquire(style)
	if existed {
		t.releaseColor(style.Fg)
		t.releaseColor(style.Bg)
		t.releaseColor(style.UnderlineColor)
	} else if id != 0 {
		for int(id) >= len(t.decoded) {
			t.decoded = append(t.decoded, uv.Style{})
		}
		t.decoded[id] = uv.Style{Fg: t.color(style.Fg), Bg: t.color(style.Bg), UnderlineColor: t.color(style.UnderlineColor), Underline: style.Underline, Attrs: style.Attrs}
	}
	return FlatCell{Text: text, Style: id, Link: t.links.acquire(c.Link), Width: c.Width}
}

func (t *flatTables) unpack(c FlatCell) uv.Cell {
	var out uv.Cell
	if c.Text&flatIndirect != 0 {
		out.Content = t.text.values[c.Text&^flatIndirect]
	} else if c.Text != 0 {
		r := rune(c.Text - 1)
		if r < utf8.RuneSelf {
			out.Content = asciiContent[r]
		} else {
			out.Content = string(r)
		}
	}
	if c.Style != 0 {
		out.Style = t.decoded[c.Style]
	}
	if c.Link != 0 {
		out.Link = t.links.values[c.Link]
	}
	out.Width = c.Width
	return out
}

func (t *flatTables) retain(c FlatCell) {
	if c.Text&flatIndirect != 0 {
		t.text.retain(c.Text &^ flatIndirect)
	}
	t.styles.retain(c.Style)
	t.links.retain(c.Link)
}

func (t *flatTables) release(c FlatCell) {
	if c.Text&flatIndirect != 0 {
		t.text.release(c.Text &^ flatIndirect)
	}
	t.links.release(c.Link)
	if s, last := t.styles.release(c.Style); last {
		t.decoded[c.Style] = uv.Style{}
		t.releaseColor(s.Fg)
		t.releaseColor(s.Bg)
		t.releaseColor(s.UnderlineColor)
		if t.styles.live == 0 {
			t.decoded = nil
		}
	}
}
