package vt

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

type plainRowPredicateSliceColor []uint16

func (c plainRowPredicateSliceColor) RGBA() (uint32, uint32, uint32, uint32) { return 1, 2, 3, 4 }
func TestPlainRowPredicateEquivalent(t *testing.T) {
	colors := []color.Color{nil, color.RGBA{R: 1, A: 255}, plainRowPredicateSliceColor{1}}
	cases := 0
	for _, text := range []string{"", " ", "x", "世", "\x00", "á"} {
		for _, width := range []int{-1, 0, 1, 2, 3} {
			for _, fg := range colors {
				for _, bg := range colors {
					for _, uc := range colors {
						for _, attrs := range []uint8{0, uv.AttrBold, uv.AttrReverse} {
							for _, underline := range []uv.Underline{uv.UnderlineNone, uv.UnderlineSingle} {
								for _, link := range []uv.Link{{}, {URL: "x"}, {Params: "p"}} {
									c := uv.Cell{Content: text, Width: width, Style: uv.Style{Fg: fg, Bg: bg, UnderlineColor: uc, Attrs: attrs, Underline: underline}, Link: link}
									want := (width == 1 && len(text) == 1 && text[0] < 128) && fg == nil && bg == nil && uc == nil && attrs == 0 && underline == uv.UnderlineNone && link.URL == "" && link.Params == ""
									if plainRow(uv.Line{c}) != want {
										t.Fatalf("c=%+v want=%v", c, want)
									}
									cases++
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("predicate matched in %d cases including noncomparable colors", cases)
}
