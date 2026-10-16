package render

import (
	"github.com/kazuph/goterrace/internal/kitty"
	"reflect"
)

type Placeholder struct {
	Pane, X, Y, Row, Col int
	ImageID              uint32
	PlacementID          uint32
}

func panePlaceholders(f *Frame, pane int, snap *PaneSnap, x, y, width, height int) {
	used := map[[2]uint32]bool{}
	for row, cells := range snap.Grid {
		if row >= height {
			break
		}
		left := kitty.PlaceholderCell{}
		for col, cell := range cells {
			decoded := kitty.DecodePlaceholder(cell, left)
			left = decoded
			if col >= width || !decoded.Valid {
				continue
			}
			p := Placeholder{Pane: pane, X: x + col, Y: y + row, Row: decoded.Row, Col: decoded.Col}
			for _, prototype := range snap.Graphics {
				if !prototype.Virtual || prototype.Image.ID != decoded.ImageID || decoded.PlacementID != 0 && prototype.ID != decoded.PlacementID || decoded.Row >= prototype.Rows || decoded.Col >= prototype.Cols {
					continue
				}
				p.ImageID, p.PlacementID = decoded.ImageID, prototype.ID
				key := [2]uint32{prototype.Image.ID, prototype.ID}
				if !used[key] {
					used[key] = true
					f.Graphics = append(f.Graphics, Graphic{Pane: pane, Placement: prototype})
				}
				break
			}
			f.Placeholders = append(f.Placeholders, p)
		}
	}
}

// Rewrite only the frame's copy of the cell. The pane keeps its original ID
// and source row/column for handoff, scrollback, and another connected viewer.
func (e *Encoder) prepareGraphics(f *Frame, full bool) bool {
	if len(f.Graphics) == 0 && len(e.graphicsIDs) == 0 && len(f.Placeholders) == 0 {
		return false
	}
	changed := full || e.graphicsNamespace != f.GraphicsNamespace || !graphicsEqual(e.graphics, f.Graphics)
	if changed {
		previous := e.graphicsMap
		e.graphicsMap = map[string]uint32{}
		for _, g := range f.Graphics {
			key := graphicKey(g.Pane, g.Placement.Image.ID)
			if _, exists := e.graphicsMap[key]; !exists {
				if id := g.Placement.Image.NativeID; id != 0 {
					e.graphicsMap[key] = id
					continue
				}
				if e.graphicsNamespace == f.GraphicsNamespace && !removedDefaultPrototype(e.graphics, f.Graphics, g) {
					for _, old := range e.graphics {
						if old.Pane == g.Pane && reflect.DeepEqual(old.Placement.Image, g.Placement.Image) {
							e.graphicsMap[key] = previous[key]
							break
						}
					}
				}
				if e.graphicsMap[key] != 0 {
					continue
				}
				for {
					e.nextGraphicID++
					if _, reserved := e.nativeImages[e.nextGraphicID]; e.nextGraphicID != 0 && !reserved {
						break
					}
				}
				e.graphicsMap[key] = e.nextGraphicID
			}
		}
	}
	for _, p := range f.Placeholders {
		cell := f.Cell(p.X, p.Y)
		if cell == nil || len([]rune(cell.Content)) == 0 || []rune(cell.Content)[0] != kitty.PlaceholderRune {
			continue
		}
		id := e.graphicsMap[graphicKey(p.Pane, p.ImageID)]
		if id == 0 {
			cell.Content = " "
			continue
		}
		if normalized := kitty.PlaceholderText(p.Row, p.Col, id); normalized != "" {
			cell.Content = normalized
		}
		cell.Style.Fg = kitty.PlaceholderColor(id)
		cell.Style.UnderlineColor = kitty.PlaceholderColor(p.PlacementID)
	}
	return changed
}
