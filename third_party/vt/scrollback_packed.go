package vt

import uv "github.com/charmbracelet/ultraviolet"

// Plain single-column rows need only their bytes. Other rows retain cells so
// graphemes, continuation columns, styles and hyperlinks are lossless.
func plainRow(line uv.Line) bool {
	for i := range line {
		c := &line[i]
		if c.Width != 1 || len(c.Content) != 1 || c.Content[0] >= 128 ||
			c.Style.Fg != nil || c.Style.Bg != nil || c.Style.UnderlineColor != nil ||
			c.Style.Underline != uv.UnderlineNone || c.Style.Attrs != 0 ||
			c.Link.URL != "" || c.Link.Params != "" {
			return false
		}
	}
	return len(line) > 0
}
func unpackRow(text []byte) uv.Line {
	row := make(uv.Line, len(text))
	for x, b := range text {
		row[x] = uv.Cell{Content: string(b), Width: 1}
	}
	return row
}
