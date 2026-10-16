package vt

import uv "github.com/charmbracelet/ultraviolet"

// Plain single-column rows need only their bytes. Styled rows use runs in
// scrollback_styled.go; dense changes can still retain the original cells.
func plainRow(line uv.Line) bool {
	for i := range line {
		c := &line[i]
		if c.Width != 1 || len(c.Content) != 1 || c.Content[0] >= 128 ||
			int(c.Style.Attrs)|int(c.Style.Underline)|len(c.Link.URL)|len(c.Link.Params) != 0 ||
			c.Style.Fg != nil || c.Style.Bg != nil || c.Style.UnderlineColor != nil {
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
