package vt

import (
	"image/color"
	"math/bits"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestReflowShapeCheckpointThresholdPreservesBoundsAndCharge(t *testing.T) {
	for _, rows := range []int{bits.UintSize - 1, bits.UintSize, bits.UintSize + 1, 2*bits.UintSize + 1} {
		line := make(uv.Line, rows)
		for i := range line {
			line[i] = uv.EmptyCell
		}
		line[rows-1] = uv.Cell{Content: "x", Width: 1}
		shape := newReflowShape(line)
		for row := 0; row < rows; row++ {
			start, end, ok := shape.bounds(1, row)
			if !ok || start != row || end != row+1 || shape.prefixBytes(1, row) != row*scrollbackRowBytes {
				t.Fatalf("rows=%d row=%d changed bounds or prefix", rows, row)
			}
		}
		if shape.cost(1) != (reflowCost{rows, rows*scrollbackRowBytes + scrollbackCellBytes}) {
			t.Fatal("total byte charge changed")
		}
		for width := 1; width <= rows; width++ {
			if (len(shape.checkpoints[width]) > 0) != (shape.cost(width).rows > bits.UintSize) {
				t.Fatalf("rows=%d width=%d retained unnecessary or missing checkpoint", rows, width)
			}
		}
	}
}

func TestReflowShapeMatchesActualRowsAndExpandedCharge(t *testing.T) {
	style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
	linked := uv.Cell{Content: "e\u0301", Width: 1, Style: style, Link: uv.Link{URL: "https://example.com"}}
	cases := []uv.Line{nil, {{Content: "x", Width: 1}}, {{Content: "界", Width: 2}, {}, {Content: "x", Width: 1}}}
	for _, mixed := range []bool{false, true} {
		var line uv.Line
		for i := 0; i < 2*bits.UintSize; i++ {
			switch {
			case mixed && i%3 == 0:
				line = append(line, uv.Cell{Content: "界", Width: 2}, uv.Cell{})
			case mixed && i%3 == 1:
				line = append(line, uv.EmptyCell)
			default:
				line = append(line, linked)
			}
		}
		line = append(line, uv.Cell{Content: "z", Width: 1})
		cases = append(cases, line)
	}
	for caseID, line := range cases {
		shape := newReflowShape(line)
		for width := 1; width <= len(line)+2; width++ {
			s := NewScreen(max(1, len(line)+1), 1)
			s.writeScreenLine(0, line)
			s.reflow(width, 1, nil, false)
			actual := append(s.scrollback.Lines(), s.buf.Line(0))
			charged := NewScrollback(len(actual))
			charged.SetMaxBytes(len(actual) * (scrollbackRowBytes + width*scrollbackCellBytes))
			for _, row := range actual {
				charged.Push(row)
			}
			if got := shape.cost(width); got != (reflowCost{len(actual), charged.Bytes()}) {
				t.Fatalf("case=%d width=%d cost=%v want rows=%d bytes=%d", caseID, width, got, len(actual), charged.Bytes())
			}
			prefix := 0
			for row := range actual {
				if got := shape.prefixBytes(width, row); got != prefix {
					t.Fatalf("width=%d row=%d prefix=%d want=%d", width, row, got, prefix)
				}
				prefix += charged.rowBytes[charged.wrap(charged.start+row)]
				start, end, ok := shape.bounds(width, row)
				if !ok {
					t.Fatalf("missing row %d", row)
				}
				projected := make(uv.Line, width)
				copy(projected, line[start:end])
				trimmed := NewScrollback(1)
				trimmed.Push(projected)
				if !reflect.DeepEqual(trimmed.Line(0), charged.Line(row)) && !(len(trimmed.Line(0)) == 0 && len(charged.Line(row)) == 0) {
					t.Fatalf("case=%d width=%d row=%d bounds=%d:%d", caseID, width, row, start, end)
				}
			}
			if _, _, ok := shape.bounds(width, len(actual)); ok {
				t.Fatal("out-of-range row accepted")
			}
		}
	}
}
