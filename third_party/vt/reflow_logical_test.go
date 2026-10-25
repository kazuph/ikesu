package vt

import (
	"image/color"
	"math/bits"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestReflowUnitWidthShapePreservesBlankAndAppearanceRules(t *testing.T) {
	style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
	base := uv.Line{{Content: "a", Width: 1}, uv.EmptyCell, {Content: "b", Width: 1}}
	shape := newReflowShape(base)
	for _, test := range []struct {
		name string
		line uv.Line
		want bool
	}{
		{"text and appearance", uv.Line{{Content: "e\u0301", Width: 1, Style: style, Link: uv.Link{URL: "https://example.com"}}, {}, {Content: "界", Width: 1}}, true},
		{"styled space is meaningful", uv.Line{{Content: "a", Width: 1}, {Content: " ", Width: 1, Style: style}, {Content: "b", Width: 1}}, false},
		{"linked space is meaningful", uv.Line{{Content: "a", Width: 1}, {Content: " ", Width: 1, Link: uv.Link{URL: "https://example.com"}}, {Content: "b", Width: 1}}, false},
		{"blank position changed", uv.Line{uv.EmptyCell, {Content: "a", Width: 1}, {Content: "b", Width: 1}}, false},
		{"wide glyph", uv.Line{{Content: "界", Width: 2}, {}, {Content: "b", Width: 1}}, false},
		{"short row", base[:2], false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if shape.matches(test.line) != test.want {
				t.Fatalf("shape match=%v want=%v", shape.matches(test.line), test.want)
			}
			if test.want {
				other := newReflowShape(test.line)
				for width := 1; width <= len(base)+1; width++ {
					if shape.cost(width) != other.cost(width) {
						t.Fatal("shared shape changed wrapping or byte accounting", width)
					}
				}
			}
		})
	}
}

func TestReflowShapeEntryReferencesSurviveInterleavedRelease(t *testing.T) {
	pool := &reflowShapePool{}
	row := uv.Line{{Content: "x", Width: 1}}
	a := pool.acquire(row)
	if pool.acquire(row) != a {
		t.Fatal("latest shape not reused")
	}
	b := pool.acquire(uv.Line{{Content: "界", Width: 2}, {}})
	if pool.acquire(row) != a {
		t.Fatal("non-latest shape not reused")
	}
	pool.release(b)
	if pool.latest == nil || pool.latest.shape != a || pool.latest.refs != 3 || len(pool.refs) != 1 || len(pool.general) != 0 {
		t.Fatal("releasing non-latest changed live references")
	}
	pool.release(a)
	pool.release(a)
	if pool.latest.refs != 1 || pool.refs[a] != pool.latest {
		t.Fatal("live shape was evicted early")
	}
	pool.release(a)
	if pool.latest != nil || len(pool.refs)+len(pool.plain)+len(pool.general) != 0 {
		t.Fatal("last release retained cache entry")
	}
}

func TestReflowLogicalCompactCellsAndShapeSharing(t *testing.T) {
	pool := &reflowShapePool{}
	style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
	cases := []uv.Line{
		{{Content: "a", Width: 1}, {Content: "b", Width: 1}},
		{{Content: "x", Width: 1, Style: style}, {Content: "y", Width: 1}},
		{{Content: "界", Width: 2, Style: style, Link: uv.Link{URL: "https://example.com"}}, {}, uv.EmptyCell, {Content: "e\u0301", Width: 1}},
		{{Content: "界", Width: 2}, {}, uv.EmptyCell, {Content: "z", Width: 1}},
	}
	var lines []*reflowLogicalLine
	for _, cells := range cases {
		line := newReflowLogicalLine(cells, pool)
		lines = append(lines, line)
		for start := range cells {
			for end := start + 1; end <= len(cells); end++ {
				out := make(uv.Line, end-start)
				line.copyRange(out, start, end)
				if !reflect.DeepEqual(out, cells[start:end]) {
					t.Fatalf("range %d:%d got=%v want=%v", start, end, out, cells[start:end])
				}
			}
		}
	}
	if lines[0].shape != lines[1].shape || lines[2].shape != lines[3].shape {
		t.Fatal("text or appearance prevented shape sharing")
	}
	if len(pool.refs) != 2 {
		t.Fatal("unexpected shape count")
	}
	for _, line := range lines {
		pool.release(line.shape)
	}
	if len(pool.refs)+len(pool.plain)+len(pool.general) != 0 {
		t.Fatal("evicted shape retained by cache")
	}
}

func TestReflowMetadataSnapshotSurvivesHistoryMutations(t *testing.T) {
	shape := newReflowShape(uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}})
	var root *reflowIndex
	for i := 0; i < bits.UintSize*3; i++ {
		root = root.append(shape)
	}
	snapshot := reflowProjection{index: root, width: 2, first: 1, rows: root.cost(2).rows - 2}
	before := snapshot.bytes()
	changed := root.append(shape).dropGroups(bits.UintSize + 1).takeGroups(bits.UintSize / 2)
	if changed.groups != bits.UintSize/2 || root.groups != bits.UintSize*3 || snapshot.bytes() != before {
		t.Fatal("mutation changed preceding metadata snapshot")
	}
	for row := 0; row < snapshot.rows; row++ {
		group, start, end, flag, ok := snapshot.row(row)
		physical := row + 1
		if !ok || group != physical/2 || flag.Continued != (physical%2 == 1) {
			t.Fatalf("row=%d group=%d flag=%v", row, group, flag)
		}
		if physical%2 == 0 && (start != 0 || end != 2 || flag.Width != 2) || physical%2 == 1 && (start != 2 || end != 3 || flag.Width != 1) {
			t.Fatalf("row=%d bounds=%d:%d flag=%v", row, start, end, flag)
		}
	}
}
