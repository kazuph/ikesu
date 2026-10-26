package vt

import (
	"math/bits"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPreparedChurnKeepsRetiredNodesWithinLiveTreeBudget(t *testing.T) {
	h := &reflowHistory{}
	keep := bits.UintSize * 3
	appendLines := func(count int) {
		lines := make([]*reflowLogicalLine, count)
		for i := range lines {
			lines[i] = newReflowLogicalLine(uv.Line{{Content: "x", Width: 1}}, &h.pool)
		}
		h.appendPreparedBatchOwned(lines, true)
	}
	appendLines(keep)
	seen := map[*reflowIndex]bool{}
	for step := range bits.UintSize * bits.UintSize {
		count := step%bits.UintSize + 1
		h.dropGroupsOwned(count, true)
		appendLines(count)
		clear(seen)
		var visit func(*reflowIndex)
		visit = func(n *reflowIndex) {
			if n == nil {
				return
			}
			if _, exists := seen[n]; exists {
				t.Fatal("node is shared within the active tree")
			}
			seen[n] = true
			visit(n.left)
			visit(n.right)
		}
		visit(h.view.index)
		for _, n := range h.freeIndex {
			if _, exists := seen[n]; exists {
				t.Fatal("retired node is active or duplicated")
			}
			seen[n] = false
		}
		// Each leaf holds at least one group; a binary tree has at most
		// twice its leaf count minus one nodes, including an incoming batch.
		if len(seen) > 2*(keep+bits.UintSize)-1 {
			t.Fatalf("retired storage grew across stable-size output: step=%d active+retired=%d", step, len(seen))
		}
		if h.view.index.groups != keep || h.view.index.cost(1).rows != keep {
			t.Fatal("churn changed the retained logical groups")
		}
	}
}

func TestPreparedSmallBatchesFillLeavesWithoutChangingPublishedSnapshots(t *testing.T) {
	for _, row := range []uv.Line{
		{{Content: "x", Width: 1}},
		{{Content: "界", Width: 2}, {}, uv.EmptyCell, {Content: "e\u0301", Width: 1}},
	} {
		h := &reflowHistory{}
		count := bits.UintSize*3 + 1
		var shapes []*reflowShape
		for range count {
			line := newReflowLogicalLine(row, &h.pool)
			h.appendPreparedBatchOwned([]*reflowLogicalLine{line}, true)
			shapes = append(shapes, line.shape)
		}
		leaves := 0
		var visit func(*reflowIndex)
		visit = func(n *reflowIndex) {
			if n.left == nil {
				leaves++
				return
			}
			visit(n.left)
			visit(n.right)
		}
		visit(h.view.index)
		if leaves != (count+bits.UintSize-1)/bits.UintSize {
			t.Fatal("small output batches left sparse leaves", leaves)
		}
		published := h.view.index
		expected := reflowIndexBatch(shapes)
		h.appendPreparedBatch([]*reflowLogicalLine{newReflowLogicalLine(row, &h.pool)})
		for width := 1; width <= bits.UintSize; width++ {
			before, added := expected.cost(width), shapes[0].cost(width)
			want := reflowCost{before.rows + added.rows, before.bytes + added.bytes}
			if published.cost(width) != before || h.view.index.cost(width) != want {
				t.Fatal("leaf packing or published append changed width costs", width)
			}
		}
	}
}

func TestPreparedShapeSurvivesWholeBatchReplacementOnlyWhileReused(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		s := NewScreen(6, 2)
		s.scrollback = NewScrollback(2)
		if bounded {
			s.scrollback.SetMaxBytes(2 * (scrollbackRowBytes + 3*scrollbackCellBytes))
		}
		get := func(int) ReflowRow { return ReflowRow{} }
		push := func(row uv.Line) {
			for range 2 {
				s.scrollback.Push(row)
			}
			s.prepareReflowHistory(get)
		}
		push(uv.Line{{Content: "a", Width: 1}, uv.EmptyCell, {Content: "b", Width: 1}})
		h := s.scrollback.prepared.history
		shape := h.at(0).shape
		push(uv.Line{{Content: "x", Width: 1}, uv.EmptyCell, {Content: "y", Width: 1}})
		if h.at(0).shape != shape || h.at(1).shape != shape || len(h.pool.refs) != 1 || h.pool.refs[shape].refs != 2 || s.scrollback.Line(0)[0].Content != "x" {
			t.Fatal("replaced batch lost reusable shape or changed physical cells", bounded)
		}
		push(uv.Line{{Content: "界", Width: 2}, {}, {Content: "z", Width: 1}})
		if h.pool.refs[shape] != nil || len(h.pool.refs) != 1 || h.pool.latest.refs != 2 {
			t.Fatal("temporary shape reference survived a different batch", bounded)
		}
	}
}

func TestPreparedIndexRecyclesOnlyRetiredNodes(t *testing.T) {
	h := &reflowHistory{}
	appendRows := func(count int) {
		lines := make([]*reflowLogicalLine, count)
		for i := range lines {
			row := uv.Line{{Content: "x", Width: 1}}
			if i%2 == 1 {
				row = append(row, uv.EmptyCell)
			}
			lines[i] = newReflowLogicalLine(row, &h.pool)
		}
		h.appendPreparedBatchOwned(lines, true)
	}
	appendRows(bits.UintSize * 8)
	for cycle := 0; cycle < 3; cycle++ {
		h.dropGroupsOwned(bits.UintSize*4, true)
		free := map[*reflowIndex]bool{}
		for _, node := range h.freeIndex {
			if node.left != nil || node.right != nil || len(node.shapes) != 0 || len(node.events) != 0 {
				t.Fatal("retired node retained references")
			}
			free[node] = true
		}
		if len(free) == 0 {
			t.Fatal("complete retired subtrees were not recycled")
		}
		var visit func(*reflowIndex, bool) int
		visit = func(node *reflowIndex, before bool) int {
			if node == nil {
				return 0
			}
			reused := 0
			if free[node] {
				if before {
					t.Fatal("active tree entered free pool")
				}
				reused++
			}
			return reused + visit(node.left, before) + visit(node.right, before)
		}
		visit(h.view.index, true)
		appendRows(bits.UintSize * 4)
		if visit(h.view.index, false) == 0 {
			t.Fatal("new batch did not reuse retired nodes")
		}
		shapes := make([]*reflowShape, h.count)
		for i := range shapes {
			shapes[i] = h.at(i).shape
		}
		expected := reflowIndexBatch(shapes)
		for width := 1; width <= bits.UintSize; width++ {
			if h.view.index.cost(width) != expected.cost(width) {
				t.Fatal("recycled index changed width charge")
			}
		}
	}
}

func TestPreparedHistoryFirstWidthChangeReusesClosedLines(t *testing.T) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		s := unindexedHistoryFixture(rows)
		before, charged := s.scrollback.Lines(), s.scrollback.Bytes()
		get := func(int) ReflowRow { return ReflowRow{} }
		s.prepareReflowHistory(get)
		h := s.scrollback.prepared.history
		middle, serial := h.at(rows/2), h.clipping.serial
		if s.scrollback.indexed != nil || !reflect.DeepEqual(s.scrollback.Lines(), before) || s.scrollback.Bytes() != charged {
			t.Fatal("preparation changed physical history")
		}
		view := s.reflowIndexed(159, 45, get, false, true)
		if view.HistoryLen() != rows || s.scrollback.indexed != h || h.at(rows/2) != middle || h.clipping.serial != serial+45 {
			t.Fatal("first width change rebuilt prepared history")
		}
	}
}

func TestPreparedHistoryOversizedOutputReleasesRetiredCells(t *testing.T) {
	s := NewScreen(6, 2)
	s.scrollback.SetMaxBytes(scrollbackRowBytes + scrollbackCellBytes)
	s.scrollback.Push(uv.Line{{Content: "x", Width: 1}})
	get := func(int) ReflowRow { return ReflowRow{} }
	s.prepareReflowHistory(get)
	h := s.scrollback.prepared.history
	s.scrollback.Push(uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}})
	s.prepareReflowHistory(get)
	if h.count != 0 || len(h.pool.refs) != 0 {
		t.Fatal("discarded history retained cells or shapes")
	}
}

func TestPreparedPayloadReuseMatchesPhysicalHistoryOnActivation(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		old, indexed := NewScreen(6, 2), NewScreen(6, 2)
		for _, s := range []*Screen{old, indexed} {
			s.scrollback = NewScrollback(2)
			if bounded {
				s.scrollback.SetMaxBytes(2 * (scrollbackRowBytes + 6*scrollbackCellBytes))
			}
			s.cur.Y = 1
		}
		get := func(int) ReflowRow { return ReflowRow{} }
		for index, text := range []string{"abcdef", " gh ij", "klmnop", "q rst ", "uvwxyz"} {
			var row uv.Line
			for _, r := range text {
				row = append(row, uv.Cell{Content: string(r), Width: 1})
			}
			if index%2 == 1 {
				for x := range row {
					row[x].Style.Attrs = uv.AttrBold
				}
			}
			old.scrollback.Push(row)
			indexed.scrollback.Push(row)
			indexed.prepareReflowHistory(get)
		}
		flags := old.referenceStreamingReflow(4, 2, nil, false)
		view := indexed.reflowIndexed(4, 2, get, false, true)
		if !reflect.DeepEqual(screenTestLines(old.buf), screenTestLines(indexed.buf)) || !reflect.DeepEqual(old.scrollback.Lines(), indexed.scrollback.Lines()) || !reflect.DeepEqual(flags, view.materialize()) {
			t.Fatal("reused payload changed activation contents")
		}
	}
}

func TestPreparedHistoryEvictionAndPullPreserveFirstResize(t *testing.T) {
	old, indexed := NewScreen(6, 2), NewScreen(6, 2)
	for _, s := range []*Screen{old, indexed} {
		s.scrollback.SetMaxBytes(10000000)
		for _, text := range []string{"abcdef", "ghijkl", "mnopqr", "stuvwx", "yzABCD", "EFGHIJ"} {
			var row uv.Line
			for _, r := range text {
				row = append(row, uv.Cell{Content: string(r), Width: 1})
			}
			s.scrollback.Push(row)
		}
		s.cur.Y = 1
	}
	flags := []ReflowRow{{Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {Continued: true, Width: 6}, {}, {}}
	get := func(y int) ReflowRow {
		if y < len(flags) {
			return flags[y]
		}
		return ReflowRow{}
	}
	indexed.prepareReflowHistoryBatch(get)
	for _, s := range []*Screen{old, indexed} {
		s.scrollback.dropOldest()
		s.writeScreenLine(0, s.scrollback.popLast())
	}
	flags = flags[1:]
	for _, size := range [][2]int{{4, 2}, {8, 3}, {3, 2}} {
		view := indexed.reflowIndexed(size[0], size[1], get, false, true)
		flags = old.referenceStreamingReflow(size[0], size[1], flags, false)
		if !reflect.DeepEqual(screenTestLines(old.buf), screenTestLines(indexed.buf)) || !reflect.DeepEqual(old.scrollback.Lines(), indexed.scrollback.Lines()) || old.cur.Position != indexed.cur.Position || old.scrollback.Bytes() != indexed.scrollback.Bytes() {
			t.Fatalf("size=%v output differed", size)
		}
		if !reflect.DeepEqual(flags, view.materialize()) {
			t.Fatal("metadata differed")
		}
	}
}

// Small output bursts retain cells immediately but share one index leaf build.
func TestPreparedSmallOutputsBatchIndexBeforeFirstResize(t *testing.T) {
	s := NewScreen(8, 2)
	get := func(int) ReflowRow { return ReflowRow{} }
	for i := 0; i < 3; i++ {
		s.scrollback.Push(uv.Line{{Content: string(rune('a' + i)), Width: 1}})
		s.prepareReflowHistoryBatch(get)
	}
	p := s.scrollback.prepared
	if p.history.view.index != nil {
		t.Fatal("small outputs built an index before a full leaf or resize")
	}
	reference := NewScreen(8, 2)
	for _, row := range s.scrollback.Lines() {
		reference.scrollback.Push(row)
	}
	flags := reference.referenceStreamingReflow(7, 2, nil, false)
	view := s.reflowIndexed(7, 2, get, false, true)
	if !reflect.DeepEqual(view.materialize(), flags) || !reflect.DeepEqual(s.scrollback.Lines(), reference.scrollback.Lines()) || !reflect.DeepEqual(screenTestLines(s.buf), screenTestLines(reference.buf)) {
		t.Fatal("first resize lost pending history")
	}
}
