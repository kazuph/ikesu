package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// HelpLines wraps the measured binding and command columns while retaining each cell's style.
func HelpLines(rows []HelpRow, width int, palette Palette) [][]uv.Cell {
	width = max(2, width)
	var lines [][]uv.Cell
	for _, row := range rows {
		if row.Bind == "" && row.Cmd == "" {
			lines = append(lines, nil)
			continue
		}
		length := max(1+ansi.StringWidth(row.Bind), 30+ansi.StringWidth(row.Cmd))
		if row.Group {
			length = 1 + ansi.StringWidth(row.Cmd)
		}
		f := NewFrame(length, 1)
		f.Fill(0, 0, length, 1, " ", uv.Style{Bg: palette.PopupBG})
		if row.Group {
			f.Text(0, 0, " "+row.Cmd, uv.Style{Fg: palette.Accent, Bg: palette.PopupBG, Attrs: uv.AttrBold})
		} else {
			bind := uv.Style{Fg: palette.Mauve, Bg: palette.PopupBG, Attrs: uv.AttrBold}
			f.Fill(0, 0, 30, 1, " ", bind)
			f.Text(1, 0, row.Bind, bind)
			f.Text(30, 0, row.Cmd, uv.Style{Fg: palette.Text, Bg: palette.PopupBG})
		}
		remaining := f.Cells
		for len(remaining) > width {
			cut := width
			continuedPadding := remaining[width-1].Content == " " && remaining[width].Content == " "
			wordBoundary := remaining[width].Content == " "
			if wordBoundary {
				cut++
			}
			if remaining[cut-1].Width == 2 {
				cut--
			}
			if !wordBoundary {
				for i := cut - 1; i > 0; i-- {
					if remaining[i].Content == " " {
						cut = i
						break
					}
				}
			}
			end := min(cut, width)
			for end > 0 && remaining[end-1].Content == " " {
				end--
			}
			lines = append(lines, remaining[:end])
			remaining = remaining[cut:]
			for !continuedPadding && len(remaining) > 0 && remaining[0].Content == " " {
				remaining = remaining[1:]
			}
		}
		lines = append(lines, remaining)
	}
	return lines
}
