package vt

import (
	"encoding/binary"
	"sort"

	uv "github.com/charmbracelet/ultraviolet"
)

type reflowShapePool struct {
	plain   map[int]*reflowShapeEntry
	general map[string]*reflowShapeEntry
	refs    map[*reflowShape]*reflowShapeEntry
	latest  *reflowShapeEntry
}

type reflowShapeEntry struct {
	shape *reflowShape
	refs  int
	key   string
}

func (p *reflowShapePool) acquire(line uv.Line) *reflowShape {
	if p.latest != nil && p.latest.shape.matches(line) {
		p.latest.refs++
		return p.latest.shape
	}
	if p.refs == nil {
		p.plain = make(map[int]*reflowShapeEntry)
		p.general = make(map[string]*reflowShapeEntry)
		p.refs = make(map[*reflowShape]*reflowShapeEntry)
	}
	plain := true
	for i := 0; i < len(line); i++ {
		plain = plain && line[i].Width == 1 && !blankCell(&line[i])
	}
	var key string
	var entry *reflowShapeEntry
	if plain {
		entry = p.plain[len(line)]
	} else {
		var data []byte
		for i := 0; i < len(line); {
			span := max(line[i].Width, 1)
			value := uint64(span) << 1
			if !blankCell(&line[i]) {
				value++
			}
			data = binary.AppendUvarint(data, value)
			i += span
		}
		key = string(data)
		entry = p.general[key]
	}
	if entry == nil {
		entry = &reflowShapeEntry{shape: newReflowShape(line), key: key}
		p.refs[entry.shape] = entry
		if plain {
			p.plain[len(line)] = entry
		} else {
			p.general[key] = entry
		}
	}
	entry.refs++
	p.latest = entry
	return entry.shape
}

// Remove only the cache reference. Immutable metadata snapshots may still use
// the shape, without keeping any evicted text or appearance alive.
func (p *reflowShapePool) release(shape *reflowShape) {
	entry := p.latest
	if entry == nil || entry.shape != shape {
		entry = p.refs[shape]
	}
	entry.refs--
	if entry.refs != 0 {
		return
	}
	delete(p.refs, shape)
	if p.latest == entry {
		p.latest = nil
	}
	if shape.plain {
		delete(p.plain, shape.length)
		return
	}
	delete(p.general, entry.key)
}

func (s *reflowShape) matches(line uv.Line) bool {
	if s.plain {
		if len(line) != s.length {
			return false
		}
		for i := range line {
			if line[i].Width != 1 || blankCell(&line[i]) {
				return false
			}
		}
		return true
	}
	if s.maxSpan == 1 {
		if len(line) != s.length {
			return false
		}
		for i := range line {
			if line[i].Width > 1 || (s.last[i+1] == i) == blankCell(&line[i]) {
				return false
			}
		}
		return true
	}
	position := 0
	for i := 0; i < len(line); {
		span := max(line[i].Width, 1)
		if position+span > s.length || s.heads[position] != position || s.heads[position+span-1] != position || (s.last[position+1] == position) == blankCell(&line[i]) {
			return false
		}
		position += span
		i += span
	}
	return position == s.length
}

type reflowLogicalLine struct {
	shape      *reflowShape
	pooled     *reflowShape
	packed     []byte
	styled     *styledRow
	dense      uv.Line
	clipping   *reflowClipping
	serial     uint64
	sourceRows int
}

type reflowClipping struct {
	serial  uint64
	through map[int]uint64
}

func (l *reflowLogicalLine) ownedShape() *reflowShape {
	if l.pooled != nil {
		return l.pooled
	}
	return l.shape
}

func newReflowLogicalLine(line uv.Line, pool *reflowShapePool) *reflowLogicalLine {
	l := &reflowLogicalLine{shape: pool.acquire(line)}
	if plainRow(line) {
		l.packed = make([]byte, len(line))
		for x := range line {
			l.packed[x] = line[x].Content[0]
		}
	} else if styled := packStyled(line, nil); styled != nil {
		l.styled = styled
	} else {
		l.dense = append(uv.Line(nil), line...)
	}
	return l
}

func (l *reflowLogicalLine) copyRange(dst uv.Line, start, end int) {
	switch {
	case l.packed != nil:
		end = min(end, len(l.packed))
		for x := start; x < end; x++ {
			dst[x-start] = uv.Cell{Content: asciiContent[l.packed[x]], Width: 1}
		}
	case l.styled != nil:
		end = min(end, len(l.styled.glyphs))
		run := sort.Search(len(l.styled.runs), func(i int) bool { return l.styled.runs[i].end > start })
		for x := start; x < end; x++ {
			for x >= l.styled.runs[run].end {
				run++
			}
			g, appearance := l.styled.glyphs[x], l.styled.runs[run]
			dst[x-start] = uv.Cell{Content: g.content, Width: g.width, Style: appearance.style, Link: appearance.link}
		}
	default:
		if start < len(l.dense) {
			copy(dst, l.dense[start:min(end, len(l.dense))])
		}
	}
	// A glyph clipped by a previous viewport lost those continuation cells in
	// physical storage. Reconstruct the same default blanks on a later reflow.
	if l.clipping != nil && !l.shape.plain {
		clipped := 0
		for width, serial := range l.clipping.through {
			if l.serial <= serial && (clipped == 0 || width < clipped) {
				clipped = width
			}
		}
		if clipped > 0 {
			for x := start; x < end; x++ {
				if x-l.shape.heads[x] >= clipped {
					dst[x-start] = uv.EmptyCell
				}
			}
		}
	}
}

// Snapshots contain only shape metadata. A height change can consume rows from
// the live history while runtime callbacks still read the preceding snapshot.
type reflowProjection struct {
	index              *reflowIndex
	width, first, rows int
	cursor             *reflowCursor
}

func (p reflowProjection) bytes() int {
	return p.index.prefixBytes(p.width, p.first+p.rows) - p.index.prefixBytes(p.width, p.first)
}

func (p *reflowProjection) row(index int) (group, start, end int, flag ReflowRow, ok bool) {
	if index < 0 || index >= p.rows {
		return 0, 0, 0, ReflowRow{}, false
	}
	if p.cursor == nil {
		p.cursor = &reflowCursor{}
	}
	position := p.cursor.locate(p.index, p.width, p.first+index)
	return position.group, position.start, position.end, position.flag, true
}

func (n *reflowIndex) shapeAt(group int) *reflowShape {
	if n.left == nil {
		return n.shapes[group]
	}
	if group < n.left.groups {
		return n.left.shapeAt(group)
	}
	return n.right.shapeAt(group - n.left.groups)
}
