package vt

import (
	"math/bits"
	"sort"

	uv "github.com/charmbracelet/ultraviolet"
)

type reflowCost struct{ rows, bytes int }
type reflowCostEvent struct {
	width int
	cost  reflowCost
}
type reflowCheckpoint struct{ start, bytes int }

// Shapes share wrapping and expanded-byte accounting independently of text,
// colors and links. The latter remain attached to the saved cells.
type reflowShape struct {
	length, maxSpan int
	plain           bool
	endMarker       bool
	heads, last     []int
	events          []reflowCostEvent
	checkpoints     map[int][]reflowCheckpoint
}

func newReflowShape(line uv.Line) *reflowShape {
	s := &reflowShape{plain: true, maxSpan: 1}
	for i := 0; i < len(line); {
		span := max(line[i].Width, 1)
		s.length += span
		s.maxSpan = max(s.maxSpan, span)
		s.plain = s.plain && span == 1 && !blankCell(&line[i])
		i += span
	}
	if s.plain {
		for width := 1; width <= max(1, s.length); {
			cost := s.cost(width)
			s.events = append(s.events, reflowCostEvent{width, cost})
			if cost.rows == 1 {
				break
			}
			width = (s.length-1)/(cost.rows-1) + 1
		}
		return s
	}
	s.heads = make([]int, s.length+1)
	s.last = make([]int, s.length+1)
	s.last[0] = -1
	for i, x := 0, 0; i < len(line); {
		span := max(line[i].Width, 1)
		meaningful := !blankCell(&line[i])
		for col := 0; col < span; col++ {
			s.heads[x] = x - col
			s.last[x+1] = s.last[x]
			if meaningful {
				s.last[x+1] = x
			}
			x++
		}
		i += span
	}
	s.heads[s.length] = s.length
	s.checkpoints = make(map[int][]reflowCheckpoint)
	for width := 1; width <= max(1, s.length); width++ {
		cost := reflowCost{}
		var checkpoints []reflowCheckpoint
		for start := s.first(width); ; {
			if cost.rows%bits.UintSize == 0 {
				checkpoints = append(checkpoints, reflowCheckpoint{start, cost.bytes})
			}
			end, painted := s.next(start, width)
			trimmed := 0
			if start >= 0 && painted > 0 {
				trimmed = max(0, s.last[start+painted]-start+1)
			}
			cost.rows++
			cost.bytes += scrollbackRowBytes + trimmed*scrollbackCellBytes
			if end >= s.length {
				break
			}
			start = end
		}
		if len(s.events) == 0 || s.events[len(s.events)-1].cost != cost {
			s.events = append(s.events, reflowCostEvent{width, cost})
		}
		if cost.rows > bits.UintSize {
			s.checkpoints[width] = checkpoints
		}
	}
	return s
}

func (s *reflowShape) cost(width int) reflowCost {
	if s.plain {
		rows := max(1, (s.length+width-1)/width)
		return reflowCost{rows, rows*scrollbackRowBytes + s.length*scrollbackCellBytes}
	}
	i := sort.Search(len(s.events), func(i int) bool { return s.events[i].width > width }) - 1
	return s.events[max(0, i)].cost
}

func (s *reflowShape) first(width int) int {
	if s.length > 0 && s.heads[min(width, s.length)] == 0 && s.maxSpan > width {
		return -1 // The existing reflow emits an empty row before this glyph.
	}
	return 0
}

func (s *reflowShape) next(start, width int) (end, painted int) {
	if start < 0 {
		return 0, 0
	}
	end = min(start+width, s.length)
	if end < s.length {
		end = s.heads[end]
	}
	if end == start && start < s.length {
		end++
		for end < s.length && s.heads[end] == start {
			end++
		}
	}
	return end, min(width, end-start)
}

// bounds walks at most bits.UintSize physical rows, even for a row deep inside
// a long logical line. No delayed whole-line projection is done on a read.
func (s *reflowShape) bounds(width, row int) (start, end int, ok bool) {
	if row < 0 || row >= s.cost(width).rows {
		return 0, 0, false
	}
	if s.plain {
		start = row * width
		return start, min(start+width, s.length), true
	}
	start = s.first(width)
	steps := row
	if checkpoints := s.checkpoints[width]; len(checkpoints) > 0 {
		start = checkpoints[row/bits.UintSize].start
		steps = row % bits.UintSize
	}
	for i := 0; i < steps; i++ {
		start, _ = s.next(start, width)
	}
	if start < 0 {
		return 0, 0, true
	}
	_, painted := s.next(start, width)
	return start, start + painted, true
}

func (s *reflowShape) prefixBytes(width, rows int) int {
	if rows >= s.cost(width).rows {
		return s.cost(width).bytes
	}
	if s.plain {
		return rows*scrollbackRowBytes + min(rows*width, s.length)*scrollbackCellBytes
	}
	start, charged, steps := s.first(width), 0, rows
	if checkpoints := s.checkpoints[width]; len(checkpoints) > 0 {
		point := checkpoints[rows/bits.UintSize]
		start, charged, steps = point.start, point.bytes, rows%bits.UintSize
	}
	for i := 0; i < steps; i++ {
		end, painted := s.next(start, width)
		trimmed := 0
		if start >= 0 && painted > 0 {
			trimmed = max(0, s.last[start+painted]-start+1)
		}
		charged += scrollbackRowBytes + trimmed*scrollbackCellBytes
		start = end
	}
	return charged
}
