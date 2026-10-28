package render

import (
	"bytes"
	"fmt"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"image"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKoiLongRunRippleCache(t *testing.T) {
	for i := 0; i < 10000; i++ {
		koiRippleCached(koiRingKey{8, 8, i, i, 1})
	}
	n := 0
	koiRingCache.Range(func(_, _ any) bool { n++; return true })
	t.Logf("10000 clicks: retained=%d", n)
	if n > 256 {
		t.Fatalf("unbounded ripple retention: %d", n)
	}
}
func TestKoiLongRunSpriteCache(t *testing.T) {
	for i := 1; i <= 10000; i++ {
		koiSpriteCached(koiSpriteKey{i % 10, 0, 0, (i-1)/10 + 1, 1})
	}
	n := 0
	koiSpriteCache.Range(func(_, _ any) bool { n++; return true })
	t.Logf("1000 dimensions × 10 varieties: retained=%d", n)
	// Small fixtures exercise a full cache without allocating large pictures.
	if n > 7680 {
		t.Fatalf("unbounded sprite retention: %d", n)
	}
}
func TestKoiLongRunTerminal(t *testing.T) {
	var e Encoder
	for i := 1; i <= 10000; i++ {
		e.encodeKoi([]Graphic{{Koi: true, Pane: -4, Placement: kitty.Placement{Image: kitty.Image{ID: uint32(i), Format: 100, Width: 8, Height: 8, Data: koiRippleCached(koiRingKey{8, 8, 0, 0, 1}).Data}}}}, false)
	}
	t.Logf("10000 images: retained=%d", len(e.koi.images))
	if len(e.koi.images) > 7936 {
		t.Fatal("unbounded terminal retention", len(e.koi.images))
	}
}
func TestKoiLongRunAgentChurn(t *testing.T) {
	var a KoiAnimation
	in := koiFixture(t, 1)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("gone:%d", i)
		a.Apply(&in, nil, time.Unix(int64(i), 0), time.Second)
		a.motion[id] = koiMotion{}
	}
	a.Apply(&in, nil, time.Unix(10001, 0), time.Second)
	t.Logf("10000 removed agents: motion=%d done=%d", len(a.motion), len(a.done))
	if len(a.motion) != 0 {
		t.Fatal("departed agent motion retained", len(a.motion))
	}
}

func TestKoiLongRunByteBudgetAndLRU(t *testing.T) {
	// Use real encoded PNGs and pixels, with a small budget to reach both
	// boundaries without allocating hundreds of megabytes in a unit test.
	pixels, png := koiSpriteCached(koiSpriteKey{0, 0, 0, 8, 8})
	size := len(pixels.Pix) + len(png.Data)
	c := koiCache[int, koiRaster]{maxEntries: 3, maxBytes: 2 * size}
	create := func() (koiRaster, int) { return koiRaster{pixels, png}, size }
	c.getOrCreate(1, create)
	c.getOrCreate(2, create)
	c.getOrCreate(1, create)
	c.getOrCreate(3, create)
	if c.bytes != 2*size || len(c.items) != 2 || c.items[1] == nil || c.items[2] != nil {
		t.Fatal("byte/LRU eviction failed", c.bytes, len(c.items))
	}
	for i := 0; i < 10000; i++ {
		c.getOrCreate(i, create)
	}
	if c.bytes > c.maxBytes || len(c.items) > c.maxEntries {
		t.Fatal("budget exceeded")
	}
	oversize := koiCache[int, koiRaster]{maxEntries: 3, maxBytes: size - 1}
	oversize.getOrCreate(1, create)
	if len(oversize.items) != 0 || oversize.bytes != 0 {
		t.Fatal("oversize raster retained")
	}
	t.Logf("10000 real rasters: bytes=%d cap=%d entries=%d; oversize retained=0", c.bytes, c.maxBytes, len(c.items))
}

func TestKoiLongRunTerminalDeletesAndReuploads(t *testing.T) {
	var e Encoder
	retained := map[int]bool{}
	commands := regexp.MustCompile(`_G([^;]*);[^]*\\`)
	apply := func(data []byte) {
		for _, m := range commands.FindAllSubmatch(data, -1) {
			fields := map[string]string{}
			for _, pair := range strings.Split(string(m[1]), ",") {
				k, v, _ := strings.Cut(pair, "=")
				fields[k] = v
			}
			id, _ := strconv.Atoi(fields["i"])
			switch fields["a"] {
			case "t":
				retained[id] = true
			case "d":
				if fields["d"] == "I" {
					delete(retained, id)
				}
			case "p":
				if !retained[id] {
					t.Fatal("placement references deleted pixels", id)
				}
			}
		}
	}
	image := koiRippleCached(koiRingKey{8, 8, 0, 0, 1})
	draw := func(id int) []byte {
		image.ID = uint32(id)
		return e.encodeKoi([]Graphic{{Koi: true, Pane: -4, Placement: kitty.Placement{Image: image}}}, false)
	}
	apply(draw(1))
	apply(draw(2))
	if bytes.Contains(draw(1), []byte("a=t,")) {
		t.Fatal("retained image uploaded twice")
	}
	for i := 3; i <= 10000; i++ {
		apply(draw(i))
		if len(retained) > koiTerminalEntries {
			t.Fatal("terminal exceeded budget", len(retained))
		}
	}
	data := draw(1)
	if !bytes.Contains(data, []byte("a=t,")) {
		t.Fatal("evicted image was not uploaded again")
	}
	apply(data)
	apply(e.RestoreCursor())
	if len(retained) != 0 || len(e.koi.images) != 0 {
		t.Fatal("disconnect retained terminal images", len(retained))
	}
	t.Log("10000 uploads: terminal deletion respected; retained reuse and evicted reupload verified; disconnect retains zero")
}

func TestKoiLongRunMachineReconnectAndDoneState(t *testing.T) {
	var clock KoiAnimation
	in := koiFixture(t, 1)
	in.Panes[1].Status.Key = "done"
	groups := MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &in}, {ID: "remote", Input: &in}}}
	now := time.Unix(100, 0)
	for i := 0; i < 10000; i++ {
		groups.Groups[1].ID = fmt.Sprintf("remote-%d", i)
		clock.Apply(&in, &groups, now, time.Second)
		for _, a := range KoiAgentsFor(in, &groups) {
			koiPondPose(a, layout.Rect{W: 30, H: 40}, in)
		}
		if len(clock.done) != 2 || len(clock.motion) != 2 {
			t.Fatal("reconnect leaked state", len(clock.done), len(clock.motion))
		}
	}
	// Disconnect, then pane removal. Both gold and motion must disappear;
	// a reconnect must start its own gold expiry rather than inherit old state.
	groups.Groups[1].Input = nil
	clock.Apply(&in, &groups, now.Add(time.Second), time.Second)
	if len(clock.done) != 1 || len(clock.motion) != 1 {
		t.Fatal("disconnect retained remote agent")
	}
	in.Panes = map[int]*PaneSnap{}
	clock.Apply(&in, &groups, now.Add(2*time.Second), time.Second)
	if len(clock.done) != 0 || len(clock.motion) != 0 {
		t.Fatal("removed panes retained state")
	}
	t.Log("10000 endpoint replacements: done=2 motion=2; disconnect=1; pane removal=0")
}

func TestKoiLongRunTerminalByteBudgetAndVisibleFrame(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1024, 1024))); err != nil {
		t.Fatal(err)
	}
	picture := kitty.Image{Format: 100, Width: 1024, Height: 1024, Data: encoded.Bytes()}
	graphic := func(id int) Graphic {
		picture.ID = uint32(id)
		return Graphic{Koi: true, Pane: -4, Placement: kitty.Placement{Image: picture}}
	}
	var e Encoder
	for i := 1; i <= 100; i++ {
		e.encodeKoi([]Graphic{graphic(i)}, false)
		if e.koi.bytes > koiRingBytes {
			t.Fatal("terminal ripple byte budget exceeded")
		}
	}
	t.Logf("100 large real PNGs: retained=%d bytes=%d cap=%d", len(e.koi.images), e.koi.bytes, koiRingBytes)
	var visible []Graphic
	for i := 101; i <= 140; i++ {
		visible = append(visible, graphic(i))
	}
	e.encodeKoi(visible, false)
	// Keeping a whole frame visible takes precedence over retaining old frames.
	if len(e.koi.images) != len(visible) {
		t.Fatal("old frames retained beside an oversized visible frame")
	}
	for _, g := range visible {
		p := g.Placement.Image
		if e.koi.images[koiImageKey{-4, p.ID, p.Width, p.Height}] == 0 {
			t.Fatal("visible image evicted")
		}
	}
	e.encodeKoi(nil, false)
	if e.koi.bytes > koiRingBytes {
		t.Fatal("oversized frame still pinned after disappearing")
	}
	e.RestoreCursor()
	if e.koi.bytes != 0 {
		t.Fatal("disconnect retained byte accounting")
	}
}
func TestKoiLongRunSidebarToggle(t *testing.T) {
	var e Encoder
	img := koiRippleCached(koiRingKey{8, 8, 0, 0, 1})
	g := Graphic{Koi: true, Pane: -4, Placement: kitty.Placement{Image: img}}
	for i := 0; i < 10000; i++ {
		data := e.encodeKoi([]Graphic{g}, true)
		if i > 0 && bytes.Contains(data, []byte("a=t,")) {
			t.Fatal("sidebar toggle resent retained pixels")
		}
		e.encodeKoi(nil, false)
	}
	if len(e.koi.images) != 1 || e.koi.pools[1].order.Len() != 1 || len(e.koi.placements) != 0 {
		t.Fatal("sidebar toggles accumulated images/placements")
	}
	t.Log("10000 hide/show or off/on compositions: retained images=1 placements=0, no repeat upload")
}

func TestKoiLongRunRippleBudgetCannotEvictFish(t *testing.T) {
	var e Encoder
	img := koiRippleCached(koiRingKey{8, 8, 0, 0, 1})
	fish := Graphic{Koi: true, Pane: -1, Placement: kitty.Placement{Image: img}}
	e.encodeKoi([]Graphic{fish}, false)
	for i := 1; i <= 10000; i++ {
		wave := fish
		wave.Pane = -4
		wave.Placement.Image.ID = uint32(i)
		e.encodeKoi([]Graphic{wave}, false)
	}
	rings := 0
	for key := range e.koi.images {
		if key.Pane == -4 {
			rings++
		}
	}
	if rings > koiRingEntries {
		t.Fatal("wave images exceeded their independent budget", rings)
	}
	if bytes.Contains(e.encodeKoi([]Graphic{fish}, false), []byte("a=t,")) {
		t.Fatal("wave churn evicted a retained fish")
	}
	t.Logf("10000 waves retain %d wave images; absent fish remains reusable without upload", rings)
}
