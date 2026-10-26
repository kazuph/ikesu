//go:build flatcells

package vt

import (
	"fmt"
	"image/color"
	"reflect"
	"testing"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type flatSliceColor []uint8

func (c flatSliceColor) RGBA() (uint32, uint32, uint32, uint32) {
	return uint32(c[0]) * 257, uint32(c[1]) * 257, uint32(c[2]) * 257, 65535
}

func TestFlatCellLayoutHasNoPointers(t *testing.T) {
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				check(typ.Field(i).Type)
			}
		case reflect.Array:
			check(typ.Elem())
		case reflect.Uint32, reflect.Int:
		default:
			t.Fatalf("pointer or unexpected field type: %v", typ)
		}
	}
	check(reflect.TypeFor[FlatCell]())
	if unsafe.Sizeof(FlatCell{}) != 24 {
		t.Fatalf("flat cell size = %d, want 24", unsafe.Sizeof(FlatCell{}))
	}
	t.Logf(`{"flat_cell_bytes":%d,"uv_cell_bytes":%d,"text_offset":%d,"style_offset":%d,"link_offset":%d,"width_offset":%d,"pointer_fields":0}`, unsafe.Sizeof(FlatCell{}), unsafe.Sizeof(uv.Cell{}), unsafe.Offsetof(FlatCell{}.Text), unsafe.Offsetof(FlatCell{}.Style), unsafe.Offsetof(FlatCell{}.Link), unsafe.Offsetof(FlatCell{}.Width))
}

func TestFlatCellExactTextStyleLinkAndWidth(t *testing.T) {
	colors := []color.Color{nil, ansi.BasicColor(3), ansi.IndexedColor(42), ansi.TrueColor(0x123456), ansi.TrueColor(0xff123456), ansi.RGBColor{R: 7, G: 19, B: 83}, color.RGBA{R: 11, G: 23, B: 97, A: 255}, color.NRGBA{R: 11, G: 23, B: 97, A: 255}, color.RGBA{R: 1, G: 2, B: 3, A: 7}, color.NRGBA64{R: 257, G: 513, B: 1027, A: 8191}, flatSliceColor{3, 5, 7}}
	texts := []string{"", " ", "x", "界", "e\u0301", "👩‍💻", "\U0010eeee", string([]byte{0xff, 0xfe}), "\x00"}
	var tables flatTables
	for i, c := range colors {
		for _, text := range texts {
			for _, width := range []int{0, 1, 2, -1, int(^uint(0) >> 1)} {
				cell := uv.Cell{Content: text, Width: width, Style: uv.Style{Fg: c, Bg: colors[(i+1)%len(colors)], UnderlineColor: colors[(i+2)%len(colors)], Attrs: uv.AttrBold | uv.AttrItalic, Underline: uv.UnderlineCurly}, Link: uv.Link{URL: "https://example.test/界", Params: "id=原文"}}
				packed := tables.pack(cell)
				tables.retain(packed)
				tables.release(packed)
				if got := tables.unpack(packed); !reflect.DeepEqual(got, cell) {
					t.Fatalf("cell did not round trip: %#v != %#v", got, cell)
				}
				tables.release(packed)
			}
		}
	}
	if tables.text.live != 0 || tables.styles.live != 0 || tables.links.live != 0 || tables.live != 0 || tables.colors != nil {
		t.Fatalf("tables retained released cells: %#v", tables)
	}
}

func TestFlatCellIDReuseKeepsDecodedSnapshotAndReleasesTables(t *testing.T) {
	var tables flatTables
	var saved []uv.Cell
	for round := 0; round < 3; round++ {
		var cells []FlatCell
		for i := 0; i < 200; i++ {
			cell := uv.Cell{Content: fmt.Sprintf("e\u0301-%d-%d", round, i), Width: 2, Style: uv.Style{Fg: flatSliceColor{uint8(i), 5, 7}, UnderlineColor: color.RGBA{R: uint8(i), A: 7}, Underline: uv.UnderlineDouble}, Link: uv.Link{URL: fmt.Sprintf("https://example.test/%d/%d", round, i)}}
			packed := tables.pack(cell)
			cells = append(cells, packed)
			if round == 0 {
				saved = append(saved, tables.unpack(packed))
			}
		}
		for _, c := range cells {
			tables.release(c)
		}
		if tables.text.index != nil || tables.styles.index != nil || tables.links.index != nil || tables.byColor != nil || tables.colors != nil || tables.decoded != nil {
			t.Fatal("empty screen still owns dictionary storage")
		}
	}
	for i, c := range saved {
		if c.Content != fmt.Sprintf("e\u0301-0-%d", i) || c.Link.URL != fmt.Sprintf("https://example.test/0/%d", i) {
			t.Fatal("ID reuse changed a decoded snapshot", i, c)
		}
	}
}

func TestFlatCellOpaqueColorsKeepOriginalOwnership(t *testing.T) {
	a, b := flatSliceColor{3, 5, 7}, flatSliceColor{3, 5, 7}
	var tables flatTables
	ca := tables.pack(uv.Cell{Width: 1, Style: uv.Style{Fg: a}})
	cb := tables.pack(uv.Cell{Width: 1, Style: uv.Style{Fg: b}})
	a[0] = 11
	if tables.unpack(ca).Style.Fg.(flatSliceColor)[0] != 11 || tables.unpack(cb).Style.Fg.(flatSliceColor)[0] != 3 {
		t.Fatal("equal custom colors lost distinct source ownership")
	}
	tables.release(ca)
	tables.release(cb)
}

func BenchmarkFlatCellRetainedASCII(b *testing.B) {
	var tables flatTables
	cell := tables.pack(uv.Cell{Content: "x", Width: 1})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		next := tables.pack(uv.Cell{Content: "x", Width: 1})
		tables.release(cell)
		cell = next
	}
	b.StopTimer()
	tables.release(cell)
}
