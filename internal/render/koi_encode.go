package render

import (
	"bytes"
	"container/list"
	"encoding/base64"
	"fmt"
	"reflect"
)

type koiImageKey struct {
	Pane int
	ID   uint32
	W, H int
}

// Kitty identifies a placement by the pair (image ID, placement ID).
// Number repeated placements of each sprite independently so removing a
// pond fish cannot invalidate a mini koi's placement.
type koiPlacementKey struct {
	image, placement uint32
}
type koiImageUse struct {
	key        koiImageKey
	size       int
	frame      uint64
	placements uint32
}
type koiImagePool struct {
	order list.List
	bytes int
}
type koiEncoder struct {
	images     map[koiImageKey]uint32
	placements map[koiPlacementKey]Graphic
	used       map[koiImageKey]*list.Element
	pools      [2]koiImagePool
	bytes      int
	frame      uint64
}

func withoutKoi(f *Frame) (*Frame, []Graphic) {
	regular := *f
	regular.Graphics = nil
	regular.GraphicsSources = nil
	for _, g := range f.GraphicsSources {
		if !g.Koi {
			regular.GraphicsSources = append(regular.GraphicsSources, g)
		}
	}
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

// Images remain reusable across frames and overlays while retained. Under
// pressure retire least recently used images that are absent from this frame.
func (e *Encoder) encodeKoi(graphics []Graphic, force bool) []byte {
	var next map[koiPlacementKey]Graphic
	var order []koiPlacementKey
	if len(graphics) > 0 {
		next = make(map[koiPlacementKey]Graphic, len(graphics))
		order = make([]koiPlacementKey, 0, len(graphics))
	}
	var b bytes.Buffer
	if e.koi.images == nil {
		e.koi.images = map[koiImageKey]uint32{}
		e.koi.used = map[koiImageKey]*list.Element{}
	}
	e.koi.frame++
	for _, g := range graphics {
		p := g.Placement
		key := koiImageKey{g.Pane, p.Image.ID, p.Image.Width, p.Image.Height}
		pool := &e.koi.pools[0]
		if key.Pane == koiRipplePane {
			pool = &e.koi.pools[1]
		}
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
			size := 4*p.Image.Width*p.Image.Height + len(p.Image.Data)
			e.koi.used[key] = pool.order.PushFront(&koiImageUse{key: key, size: size})
			e.koi.bytes += size
			pool.bytes += size
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
		el := e.koi.used[key]
		use := el.Value.(*koiImageUse)
		if use.frame != e.koi.frame {
			use.frame = e.koi.frame
			use.placements = 0
		}
		pool.order.MoveToFront(el)
		use.placements++
		placement := koiPlacementKey{id, use.placements}
		next[placement] = g
		order = append(order, placement)
	}
	for key := range e.koi.placements {
		if _, ok := next[key]; !ok {
			fmt.Fprintf(&b, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2;\x1b\\", key.image, key.placement)
		}
	}
	for _, key := range order {
		item := next[key]
		if !force && reflect.DeepEqual(item, e.koi.placements[key]) {
			continue
		}
		p := item.Placement
		b.WriteString(cup(p.Y, p.X))
		fmt.Fprintf(&b, "\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,z=%d,C=1,q=2", key.image, key.placement, p.Cols, p.Rows, p.Z)
		if !item.Source.Empty() {
			r := item.Source
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
	// A currently visible image is pinned until the next composition. This
	// bounds retention by the larger of the budget and one visible frame.
	// Separate budgets stop low-reuse click history from displacing fish.
	for index := range e.koi.pools {
		pool := &e.koi.pools[index]
		entries, budget := koiSpriteEntries, koiSpriteBytes
		if index == 1 {
			entries, budget = koiRingEntries, koiRingBytes
		}
		for pool.order.Len() > entries || pool.bytes > budget {
			el := pool.order.Back()
			use := el.Value.(*koiImageUse)
			if use.frame == e.koi.frame {
				break
			}
			oldest := use.key
			fmt.Fprintf(&b, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", e.koi.images[oldest])
			e.koi.bytes -= use.size
			pool.bytes -= use.size
			pool.order.Remove(el)
			delete(e.koi.images, oldest)
			delete(e.koi.used, oldest)
		}
	}
	e.koi.placements = next
	return b.Bytes()
}

// A Jobs frame takes ownership of the right sidebar even while the
// endpoint is preparing a matching-size frame for the rest of the screen.
func (e *Encoder) clearKoiPond(b *bytes.Buffer) {
	for key, g := range e.koi.placements {
		if g.Pane == koiPondPane || g.Pane == koiRipplePane {
			fmt.Fprintf(b, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2;\x1b\\", key.image, key.placement)
			delete(e.koi.placements, key)
		}
	}
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
	if f.RetainGraphics {
		e.imageFence = false
		return nil
	}
	data := e.encodeKoi(koi, false)
	e.imageFence = bytes.Contains(data, []byte("\x1b_Ga=t,"))
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
