package vt

import (
	"math/bits"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

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
