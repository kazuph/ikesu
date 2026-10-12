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
	for _, p := range snap.Graphics {
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

func (e *Encoder) emitGraphics(f *Frame, b *bytes.Buffer, full bool) {
	if !full && (len(e.graphics) == 0 && len(f.Graphics) == 0 || e.graphicsNamespace == f.GraphicsNamespace && reflect.DeepEqual(e.graphics, f.Graphics)) {
		return
	}
	e.clearGraphics(b)
	ids := map[string]uint32{}
	for _, graphic := range f.Graphics {
		p := graphic.Placement
		key := strconv.Itoa(graphic.Pane) + ":" + strconv.FormatUint(uint64(p.Image.ID), 10)
		id, exists := ids[key]
		if !exists {
			e.nextGraphicID++
			if e.nextGraphicID == 0 {
				e.nextGraphicID++
			}
			id = e.nextGraphicID
			ids[key] = id
			e.graphicsIDs = append(e.graphicsIDs, id)
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
		b.WriteString(cup(p.Y, p.X))
		fmt.Fprintf(b, "\x1b_Ga=p,i=%d,c=%d,r=%d,z=%d,C=1,q=2;\x1b\\", id, p.Cols, p.Rows, p.Z)
	}
	e.graphics, e.graphicsNamespace = f.Graphics, f.GraphicsNamespace
}
