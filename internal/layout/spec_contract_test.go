package layout

import (
	"reflect"
	"testing"
)

func assertRects(t *testing.T, l *Layout, area Rect, want map[int]Rect) {
	t.Helper()
	got := map[int]Rect{}
	for _, p := range l.Panes(area) {
		got[p.ID] = p.Rect
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rectangles = %v, want %v", got, want)
	}
}

func TestSPECMoveAndEqualize90x30(t *testing.T) {
	area := Rect{W: 90, H: 30}
	for _, tc := range []struct {
		name  string
		side  NavDir
		order []int
		rects map[int]Rect
	}{
		{"lower", NavDown, []int{1, 3, 2}, map[int]Rect{1: {0, 0, 45, 20}, 3: {45, 0, 45, 20}, 2: {0, 20, 90, 10}}},
		{"left", NavLeft, []int{2, 1, 3}, map[int]Rect{2: {0, 0, 30, 30}, 1: {30, 0, 30, 30}, 3: {60, 0, 30, 30}}},
		{"upper", NavUp, []int{2, 1, 3}, map[int]Rect{2: {0, 0, 90, 10}, 1: {0, 10, 45, 20}, 3: {45, 10, 45, 20}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := Restore(chain(Vertical, []int{1, 2, 3}), 2, []int{1, 2, 3})
			l.SetRatioAt(nil, .5)
			if !l.MoveToSide(2, tc.side) || l.Focused() != 2 || !reflect.DeepEqual(l.Order(), tc.order) {
				t.Fatalf("move: focus=%d order=%v", l.Focused(), l.Order())
			}
			assertRects(t, l, area, tc.rects)
		})
	}
	l := Restore(chain(Vertical, []int{1, 2, 3}), 2, []int{1, 2, 3})
	l.SetRatioAt(nil, .8)
	l.SetRatioAt([]bool{true}, .8)
	l.Equalize()
	assertRects(t, l, area, map[int]Rect{1: {0, 0, 30, 30}, 2: {30, 0, 30, 30}, 3: {60, 0, 30, 30}})
}

func TestSPECCycleNine180x45(t *testing.T) {
	ids := []int{1, 4, 5, 6, 7, 8, 9, 10, 11}
	l := Restore(chain(Vertical, ids), 7, ids)
	l.CyclePresets()
	want := map[int]Rect{}
	for i, id := range ids {
		want[id] = Rect{0, i * 5, 180, 5}
	}
	assertRects(t, l, Rect{W: 180, H: 45}, want)
	l.CyclePresets()
	want = map[int]Rect{}
	for i, id := range ids[:5] {
		want[id] = Rect{i * 36, 0, 36, 23}
	}
	for i, id := range ids[5:] {
		want[id] = Rect{i * 45, 23, 45, 22}
	}
	assertRects(t, l, Rect{W: 180, H: 45}, want)
	if l.Root().Ratio != .5 {
		t.Fatal("grid ratio", l.Root().Ratio)
	}
	for _, shape := range []struct {
		dir       Direction
		mainFirst bool
	}{
		{Vertical, true}, {Vertical, false}, {Horizontal, true}, {Horizontal, false},
	} {
		l.CyclePresets()
		r := l.Root()
		main := r.Second
		if shape.mainFirst {
			main = r.First
		}
		if r.Direction != shape.dir || r.Ratio != .5 || main.Leaf != ids[0] {
			t.Fatalf("main preset: %s", tree(l))
		}
	}
	l.CyclePresets()
	if tree(l) != tree(Restore(chain(Vertical, ids), 7, ids)) || l.Focused() != 7 || !reflect.DeepEqual(l.Order(), ids) {
		t.Fatal("cycle changed order/focus", tree(l), l.Order())
	}
}

func TestSPECImplicitOrderRotationAndRestore(t *testing.T) {
	ids := []int{1, 4, 5}
	l := Restore(chain(Vertical, ids), 1, ids)
	l.Rotate(true)
	if !l.SplitImplicit(Vertical, 6, .5, false) || !l.SplitImplicit(Horizontal, 7, .5, false) {
		t.Fatal("append failed")
	}
	if !reflect.DeepEqual(l.Order(), []int{1, 4, 5, 6, 7}) {
		t.Fatal("append order", l.Order())
	}
	// The logical tail (5), not the rotated tree's tail (4), receives 6.
	if tree(l) != "(r0.33 (r0.50 p5 (d0.50 p6 p7)) (r0.50 p1 p4))" {
		t.Fatal("implicit target", tree(l))
	}
	l.SplitAt(1, Vertical, 8, .5, false)
	want := []int{1, 8, 4, 5, 6, 7}
	if !reflect.DeepEqual(l.Order(), want) {
		t.Fatal("explicit order", l.Order())
	}
	l.CyclePresets()
	l = Restore(cloneNode(l.Root()), l.Focused(), l.Order())
	if !reflect.DeepEqual(l.Order(), want) {
		t.Fatal("restore order", l.Order())
	}
}

func TestSPECRotateRectPermutationAndCloseReflow(t *testing.T) {
	for _, dir := range []Direction{Vertical, Horizontal} {
		l := New(1)
		l.SplitFocused(Vertical, 2)
		l.SplitFocused(dir, 3)
		l.Focus(2)
		area := Rect{W: 90, H: 30}
		before := l.Panes(area)
		l.Rotate(true)
		assertRects(t, l, area, map[int]Rect{3: before[0].Rect, 1: before[1].Rect, 2: before[2].Rect})
		if !reflect.DeepEqual(l.LeafIDs(), []int{3, 1, 2}) || l.Focused() != 2 {
			t.Fatal("rotation order/focus")
		}
		l.Rotate(false)
		assertRects(t, l, area, map[int]Rect{1: before[0].Rect, 2: before[1].Rect, 3: before[2].Rect})
		l.Close(2)
		if l.Focused() != 3 || !reflect.DeepEqual(l.Order(), []int{1, 3}) {
			t.Fatal("close next")
		}
		l.Close(3)
		if l.Focused() != 1 {
			t.Fatal("close tail")
		}
		assertRects(t, l, area, map[int]Rect{1: area})
	}
}

func TestSPECTwoPaneCycleKeepsOrderAndFocus(t *testing.T) {
	for _, ratio := range []float64{.5, .8} {
		l := Restore(SplitNode(Vertical, ratio, LeafNode(5), LeafNode(1)), 1, []int{5, 1})
		for i := 0; i < 4; i++ {
			l.CyclePresets()
			want := Vertical
			if ratio == .5 && i%2 == 0 || ratio != .5 && i%2 == 1 {
				want = Horizontal
			}
			if l.Root().Direction != want || l.Root().Ratio != .5 || l.Focused() != 1 || !reflect.DeepEqual(l.Order(), []int{5, 1}) {
				t.Fatal("two pane contract", ratio, i, l.Fingerprint(), l.Order())
			}
		}
	}
}

func TestSPECSplitBoundaryAndRatioClamp(t *testing.T) {
	for _, dir := range []Direction{Vertical, Horizontal} {
		l := Restore(SplitNode(dir, .5, LeafNode(1), LeafNode(2)), 1, []int{1, 2})
		area := Rect{W: 90, H: 30}
		pos, other := 45, 10
		if dir == Horizontal {
			pos, other = 15, 10
		}
		for offset := -2; offset <= 2; offset++ {
			x, y := pos+offset, other
			if dir == Horizontal {
				x, y = other, pos+offset
			}
			got := l.SplitBorderAt(x, y, area)
			if (got != nil) != (offset == -1 || offset == 0) {
				t.Fatalf("boundary dir=%v offset=%d hit=%v", dir, offset, got)
			}
		}
		for _, tc := range []struct{ input, want float64 }{{-1, .1}, {0, .1}, {.1, .1}, {.5, .5}, {.9, .9}, {1, .9}, {2, .9}} {
			l.SetRatioAt(nil, tc.input)
			if l.Root().Ratio != tc.want {
				t.Fatal("ratio clamp", tc)
			}
		}
	}
}
