package render

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/kitty"
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
	for i := 1; i <= 1000; i++ {
		koiSpriteCached(koiSpriteKey{0, 0, 0, i, 1})
	}
	n := 0
	koiSpriteCache.Range(func(_, _ any) bool { n++; return true })
	t.Logf("1000 dimensions: retained=%d", n)
	// Small fixtures exercise a full cache without allocating large pictures.
	if n > 8192 {
		t.Fatalf("unbounded sprite retention: %d", n)
	}
}
func TestKoiLongRunTerminal(t *testing.T) {
	var e Encoder
	for i := 1; i <= 10000; i++ {
		e.encodeKoi([]Graphic{{Koi: true, Pane: -4, Placement: kitty.Placement{Image: kitty.Image{ID: uint32(i), Format: 100, Width: 8, Height: 8, Data: []byte{1}}}}}, false)
	}
	t.Logf("10000 images: retained=%d", len(e.koi.images))
	if len(e.koi.images) > 8192 {
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
