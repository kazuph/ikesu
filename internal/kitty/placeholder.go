package kitty

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const PlaceholderRune = '\U0010eeee'

type PlaceholderCell struct {
	ImageID, PlacementID uint32
	Row, Col             int
	Valid                bool
}

// Color numbers encode IDs, not palette RGB values, for indexed colors.
func ColorID(c color.Color) uint32 {
	switch value := c.(type) {
	case ansi.IndexedColor:
		return uint32(value)
	case ansi.BasicColor:
		return uint32(value)
	}
	if c == nil {
		return 0
	}
	r, g, b, _ := c.RGBA()
	return uint32(r>>8)<<16 | uint32(g>>8)<<8 | uint32(b>>8)
}

func PlaceholderColor(id uint32) color.Color {
	return color.RGBA{R: byte(id >> 16), G: byte(id >> 8), B: byte(id), A: 255}
}

// DecodePlaceholder resolves omitted coordinates before the viewer clips the
// row; later rendering must not infer them from a different neighboring pane.
func DecodePlaceholder(cell uv.Cell, left PlaceholderCell) PlaceholderCell {
	chars := []rune(cell.Content)
	if len(chars) == 0 || chars[0] != PlaceholderRune {
		return PlaceholderCell{}
	}
	out := PlaceholderCell{ImageID: ColorID(cell.Style.Fg), PlacementID: ColorID(cell.Style.UnderlineColor), Valid: true}
	var marks []int
	for _, c := range chars[1:] {
		for i, d := range diacritics {
			if c == d {
				marks = append(marks, i)
				break
			}
		}
	}
	if len(marks) > 0 {
		out.Row = marks[0]
	}
	if len(marks) > 1 {
		out.Col = marks[1]
	}
	high := uint32(0)
	if len(marks) > 2 {
		if marks[2] > 255 {
			return PlaceholderCell{}
		}
		high = uint32(marks[2])
	}
	same := left.Valid && left.ImageID&0xffffff == out.ImageID && left.PlacementID == out.PlacementID
	if len(marks) == 0 && same {
		out.Row, out.Col, high = left.Row, left.Col+1, left.ImageID>>24
	} else if len(marks) == 1 && same && out.Row == left.Row {
		out.Col, high = left.Col+1, left.ImageID>>24
	} else if len(marks) == 2 && same && out.Row == left.Row && out.Col == left.Col+1 {
		high = left.ImageID >> 24
	}
	out.ImageID |= high << 24
	return out
}

func PlaceholderText(row, col int, id uint32) string {
	if row < 0 || col < 0 || row >= len(diacritics) || col >= len(diacritics) {
		return ""
	}
	var text strings.Builder
	text.WriteRune(PlaceholderRune)
	text.WriteRune(diacritics[row])
	text.WriteRune(diacritics[col])
	text.WriteRune(diacritics[id>>24])
	return text.String()
}
