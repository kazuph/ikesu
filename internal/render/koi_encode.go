package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"reflect"
)

type koiImageKey struct {
	Pane int
	ID   uint32
	W, H int
}
type koiEncodedPlacement struct {
	ID      uint32
	Graphic Graphic
}
type koiEncoder struct {
	images     map[koiImageKey]uint32
	placements []koiEncodedPlacement
}

func withoutKoi(f *Frame) (*Frame, []Graphic) {
	regular := *f
	regular.Graphics = nil
	var koi []Graphic
	for _, g := range f.Graphics {
		if g.Koi {
			koi = append(koi, g)
		} else {
			regular.Graphics = append(regular.Graphics, g)
		}
	}
	return &regular, koi
}

// Koi images belong to this presenter for its whole lifetime. A frame change,
// resize, overlay or endpoint switch removes placements, never cached pixels.
func (e *Encoder) encodeKoi(graphics []Graphic, force bool) []byte {
	var next []koiEncodedPlacement
	var b bytes.Buffer
	if e.koi.images == nil {
		e.koi.images = map[koiImageKey]uint32{}
	}
	for _, g := range graphics {
		p := g.Placement
		key := koiImageKey{g.Pane, p.Image.ID, p.Image.Width, p.Image.Height}
		id := e.koi.images[key]
		if id == 0 {
			for {
				e.nextGraphicID++
				if e.nextGraphicID != 0 && !e.nativeImages[e.nextGraphicID] {
					break
				}
			}
			id = e.nextGraphicID
			e.koi.images[key] = id
			data := base64.StdEncoding.EncodeToString(p.Image.Data)
			first := true
			for len(data) > 0 {
				n := min(4096, len(data))
				more := 0
				if n < len(data) {
					more = 1
				}
				if first {
					fmt.Fprintf(&b, "\x1b_Ga=t,t=d,f=%d,s=%d,v=%d,i=%d,q=2,m=%d;%s\x1b\\", p.Image.Format, p.Image.Width, p.Image.Height, id, more, data[:n])
				} else {
					fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, data[:n])
				}
				first = false
				data = data[n:]
			}
		}
		next = append(next, koiEncodedPlacement{id, g})
	}
	for i, old := range e.koi.placements {
		if i >= len(next) || old.ID != next[i].ID {
			fmt.Fprintf(&b, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2;\x1b\\", old.ID, i+1)
		}
	}
	for i, item := range next {
		if !force && i < len(e.koi.placements) && reflect.DeepEqual(item, e.koi.placements[i]) {
			continue
		}
		p := item.Graphic.Placement
		b.WriteString(cup(p.Y, p.X))
		fmt.Fprintf(&b, "\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,z=%d,C=1,q=2", item.ID, i+1, p.Cols, p.Rows, p.Z)
		if !item.Graphic.Source.Empty() {
			r := item.Graphic.Source
			fmt.Fprintf(&b, ",x=%d,y=%d,w=%d,h=%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
		}
		if p.OffsetX != 0 {
			fmt.Fprintf(&b, ",X=%d", p.OffsetX)
		}
		if p.OffsetY != 0 {
			fmt.Fprintf(&b, ",Y=%d", p.OffsetY)
		}
		b.WriteString(";\x1b\\")
	}
	e.koi.placements = next
	return b.Bytes()
}

// KoiFrame advances only decorations. Cells, native images, hit regions and
// server acknowledgements remain owned by the last ordinary composition.
func (e *Encoder) KoiFrame(f *Frame, x, y int, visible bool) []byte {
	var koi []Graphic
	for _, g := range f.Graphics {
		if g.Koi {
			koi = append(koi, g)
		}
	}
	data := e.encodeKoi(koi, false)
	if len(data) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteString(syncStart)
	b.WriteString(hideCursor)
	b.Write(data)
	b.WriteString(cup(y, x))
	if visible {
		b.WriteString(showCursor)
	}
	b.WriteString(syncEnd)
	return b.Bytes()
}
