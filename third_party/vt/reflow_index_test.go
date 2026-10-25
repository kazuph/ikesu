package vt

import (
	"math/bits"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestUnpublishedReflowIndexOwnedUpdatesMatchPersistent(t *testing.T) {
	shapes := []*reflowShape{
		newReflowShape(nil),
		newReflowShape(uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}}),
		newReflowShape(uv.Line{{Content: "界", Width: 2}, {}, uv.EmptyCell, {Content: "x", Width: 1}}),
	}
	var owned, persistent *reflowIndex
	var verify func(*reflowIndex) int
	verify = func(n *reflowIndex) int {
		if n == nil {
			return 0
		}
		if n.left == nil {
			if n.height != 1 || n.groups != len(n.shapes) {
				t.Fatal("invalid leaf")
			}
			return 1
		}
		l, r := verify(n.left), verify(n.right)
		if l > r+1 || r > l+1 || n.height != max(l, r)+1 || n.groups != n.left.groups+n.right.groups {
			t.Fatal("invalid owned tree")
		}
		return n.height
	}
	check := func() {
		verify(owned)
		for width := 1; width <= bits.UintSize; width++ {
			if owned.cost(width) != persistent.cost(width) {
				t.Fatalf("cost width=%d", width)
			}
			for row := 0; row < owned.cost(width).rows; row++ {
				g, a, b, ok := owned.selectRow(width, row)
				pg, pa, pb, pok := persistent.selectRow(width, row)
				if g != pg || a != pa || b != pb || ok != pok || owned.prefixBytes(width, row) != persistent.prefixBytes(width, row) {
					t.Fatalf("row width=%d row=%d", width, row)
				}
			}
		}
	}
	for i := 0; i < bits.UintSize*2; i++ {
		batch := make([]*reflowShape, 1+i%bits.UintSize)
		for j := range batch {
			batch[j] = shapes[(i+j)%len(shapes)]
		}
		owned = joinReflowIndexOwned(owned, reflowIndexBatch(batch), nil, true)
		persistent = joinReflowIndex(persistent, reflowIndexBatch(batch))
		if i%3 == 0 && owned.groups > len(batch) {
			owned = owned.dropGroupsOwned(len(batch)/2, true)
			persistent = persistent.dropGroups(len(batch) / 2)
		}
		if i%5 == 0 {
			count := max(1, owned.groups-len(batch)/2)
			owned = owned.takeGroupsOwned(count, true)
			persistent = persistent.takeGroups(count)
		}
		check()
	}
	// Publication switches back to persistent updates. A retained metadata view
	// must remain readable after either end of the active history changes.
	published := owned
	before := append([]reflowCostEvent(nil), published.events...)
	owned = owned.dropGroups(owned.groups / 2).takeGroups(owned.groups / 4)
	owned = joinReflowIndex(owned, reflowIndexBatch(shapes))
	if !reflect.DeepEqual(published.events, before) {
		t.Fatal("published metadata changed")
	}
}

func TestReflowIndexAppendDropSelectAndPrefixCharge(t *testing.T) {
	plain := make(uv.Line, bits.UintSize+1)
	for x := range plain {
		plain[x] = uv.Cell{Content: "x", Width: 1}
	}
	shapes := []*reflowShape{
		newReflowShape(nil), newReflowShape(plain),
		newReflowShape(uv.Line{{Content: "界", Width: 2}, {}, uv.EmptyCell, {Content: "z", Width: 1}}),
	}
	var index *reflowIndex
	var expected []*reflowShape
	var verifyTree func(*reflowIndex) int
	verifyTree = func(n *reflowIndex) int {
		if n.left == nil {
			if n.height != 1 || n.groups != len(n.shapes) || len(n.shapes) > bits.UintSize {
				t.Fatal("invalid leaf")
			}
			return n.height
		}
		l, r := verifyTree(n.left), verifyTree(n.right)
		if l > r+1 || r > l+1 || n.height != max(l, r)+1 || n.groups != n.left.groups+n.right.groups {
			t.Fatal("invalid tree balance or count")
		}
		return n.height
	}
	verify := func() {
		verifyTree(index)
		for _, width := range []int{1, 2, 3, bits.UintSize, bits.UintSize + 2} {
			cost := reflowCost{}
			for group, shape := range expected {
				for row := 0; row < shape.cost(width).rows; row++ {
					if got := index.prefixBytes(width, cost.rows+row); got != cost.bytes+shape.prefixBytes(width, row) {
						t.Fatalf("prefix width=%d group=%d row=%d", width, group, row)
					}
					g, start, end, ok := index.selectRow(width, cost.rows+row)
					a, b, valid := shape.bounds(width, row)
					if !ok || !valid || g != group || start != a || end != b {
						t.Fatalf("select width=%d group=%d row=%d got=%d %d:%d", width, group, row, g, start, end)
					}
				}
				v := shape.cost(width)
				cost.rows += v.rows
				cost.bytes += v.bytes
			}
			if index.cost(width) != cost || index.prefixBytes(width, cost.rows) != cost.bytes {
				t.Fatal("total differs")
			}
			if _, _, _, ok := index.selectRow(width, cost.rows); ok {
				t.Fatal("out-of-range physical row accepted")
			}
		}
	}
	for i := 0; i < bits.UintSize*bits.UintSize+1; i++ {
		shape := shapes[i%len(shapes)]
		index = index.append(shape)
		expected = append(expected, shape)
	}
	verify()
	for _, drop := range []int{1, bits.UintSize, len(expected) - bits.UintSize*2} {
		index = index.dropGroups(drop)
		expected = expected[drop:]
		verify()
	}
	if index.dropGroups(len(expected)) != nil {
		t.Fatal("complete drop retained index")
	}
}
