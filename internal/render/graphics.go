package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/kazuph/goterrace/internal/kitty"
	"reflect"
	"strconv"
)

type Graphic struct {
	Pane      int
	Placement kitty.Placement
}

func paneGraphics(f *Frame, pane int, snap *PaneSnap, x, y, width, height int) {
	if len(snap.Graphics) == 0 {
		return
	}
	for _, p := range snap.Graphics {
		if p.Virtual {
			panePlaceholders(f, pane, snap, x, y, width, height)
			break
		}
	}
	for _, p := range snap.Graphics {
		if p.Virtual {
			continue
		}
		p.Y += snap.ScrollOff
		// A placement that crosses the pane edge is hidden rather than
		// drawing source pixels over the sidebar or another pane.
		if p.X < 0 || p.Y < 0 || p.X+p.Cols > width || p.Y+p.Rows > height {
			continue
		}
		p.X += x
		p.Y += y
		f.Graphics = append(f.Graphics, Graphic{Pane: pane, Placement: p})
	}
}

func (e *Encoder) clearGraphics(b *bytes.Buffer) {
	for _, id := range e.graphicsIDs {
		fmt.Fprintf(b, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id)
	}
	e.graphicsIDs = nil
}

func graphicKey(pane int, image uint32) string {
	return strconv.Itoa(pane) + ":" + strconv.FormatUint(uint64(image), 10)
}

func graphicsEqual(a, b []Graphic) bool { return reflect.DeepEqual(a, b) }

func (e *Encoder) emitGraphics(f *Frame, b *bytes.Buffer, changed bool) {
	if !changed {
		return
	}
	ids := map[string]uint32{}
	placementIDs := graphicsPlacementIDs(f.Graphics)
	for i, graphic := range f.Graphics {
		p := graphic.Placement
		key := graphicKey(graphic.Pane, p.Image.ID)
		id, exists := ids[key]
		if !exists {
			id = e.graphicsMap[key]
			ids[key] = id
			uploaded := false
			for _, oldID := range e.graphicsIDs {
				uploaded = uploaded || oldID == id
			}
			if !uploaded {
				data := base64.StdEncoding.EncodeToString(p.Image.Data)
				first := true
				for len(data) > 0 {
					// kitty specifies at most 4096 base64 bytes per APC chunk.
					n := min(len(data), 4096)
					more := 0
					if n < len(data) {
						more = 1
					}
					if first {
						fmt.Fprintf(b, "\x1b_Ga=t,t=d,f=%d,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\", p.Image.Format, p.Image.Width, p.Image.Height, id, more, data[:n])
					} else {
						fmt.Fprintf(b, "\x1b_Gm=%d;%s\x1b\\", more, data[:n])
					}
					data = data[n:]
					first = false
				}
			}
		}
		if p.Virtual {
			unchanged := false
			for _, old := range e.graphics {
				unchanged = unchanged || old.Pane == graphic.Pane && reflect.DeepEqual(old.Placement, p)
			}
			cached := false
			for _, oldID := range e.graphicsIDs {
				cached = cached || oldID == id
			}
			if unchanged && cached {
				continue
			}
			fmt.Fprintf(b, "\x1b_Ga=p,U=1,i=%d,p=%d,c=%d,r=%d,z=%d,q=2;\x1b\\", id, placementIDs[i], p.Cols, p.Rows, p.Z)
		}
	}
}

func (e *Encoder) emitRealPlacements(f *Frame, b *bytes.Buffer, changed bool) {
	if !changed {
		return
	}
	placementIDs := graphicsPlacementIDs(f.Graphics)
	for i, graphic := range f.Graphics {
		p := graphic.Placement
		if p.Virtual {
			continue
		}
		id := e.graphicsMap[graphicKey(graphic.Pane, p.Image.ID)]
		b.WriteString(cup(p.Y, p.X))
		fmt.Fprintf(b, "\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,z=%d,C=1,q=2;\x1b\\", id, placementIDs[i], p.Cols, p.Rows, p.Z)
	}
}

func (e *Encoder) retireGraphics(f *Frame, b *bytes.Buffer, changed bool) {
	if !changed {
		return
	}
	current := map[uint32]bool{}
	placements := map[[2]uint32]bool{}
	placementIDs := graphicsPlacementIDs(f.Graphics)
	for i, g := range f.Graphics {
		id := e.graphicsMap[graphicKey(g.Pane, g.Placement.Image.ID)]
		current[id] = true
		placements[[2]uint32{id, placementIDs[i]}] = true
	}
	oldIDs := map[string]uint32{}
	oldPlacementIDs := graphicsPlacementIDs(e.graphics)
	for i, g := range e.graphics {
		key := graphicKey(g.Pane, g.Placement.Image.ID)
		id, exists := oldIDs[key]
		if !exists {
			id = e.graphicsIDs[len(oldIDs)]
			oldIDs[key] = id
		}
		pid := oldPlacementIDs[i]
		if current[id] && !placements[[2]uint32{id, pid}] {
			fmt.Fprintf(b, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2;\x1b\\", id, pid)
		}
	}
	for _, id := range e.graphicsIDs {
		if !current[id] {
			fmt.Fprintf(b, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id)
		}
	}
	e.graphicsIDs = nil
	seen := map[uint32]bool{}
	for _, g := range f.Graphics {
		id := e.graphicsMap[graphicKey(g.Pane, g.Placement.Image.ID)]
		if !seen[id] {
			seen[id] = true
			e.graphicsIDs = append(e.graphicsIDs, id)
		}
	}
	e.graphics, e.graphicsNamespace = f.Graphics, f.GraphicsNamespace
}

func graphicsPlacementIDs(graphics []Graphic) []uint32 {
	ids := make([]uint32, len(graphics))
	reserved := map[uint32]bool{}
	for i, g := range graphics {
		if g.Placement.Virtual {
			ids[i] = g.Placement.ID
			reserved[ids[i]] = true
		}
	}
	next := uint32(0)
	for i, g := range graphics {
		if !g.Placement.Virtual {
			for next++; reserved[next]; next++ {
			}
			ids[i] = next
		}
	}
	return ids
}

// Kitty's placement ID zero cannot be deleted alone: it selects every
// placement of the image. Retire that prototype with its old image instead.
func removedDefaultPrototype(before, after []Graphic, image Graphic) bool {
	hadDefault := false
	for _, g := range before {
		hadDefault = hadDefault || g.Pane == image.Pane && g.Placement.Image.ID == image.Placement.Image.ID && g.Placement.Virtual && g.Placement.ID == 0
	}
	if !hadDefault {
		return false
	}
	for _, g := range after {
		if g.Pane == image.Pane && g.Placement.Image.ID == image.Placement.Image.ID && g.Placement.Virtual && g.Placement.ID == 0 {
			return false
		}
	}
	return true
}
