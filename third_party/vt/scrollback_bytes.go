package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"unsafe"
)

// The approved byte budget counts the row as expanded cells, even when its
// plain text is stored packed. Compression preserves the retained row count.
// Byte limits account for the row slice and its expanded cell backing storage.
// A cell includes character/link headers and color/style values; immutable
// referenced strings and colors are shared with the emulator, not cloned.
const scrollbackRowBytes = int(unsafe.Sizeof(uv.Line{}))
const scrollbackCellBytes = int(unsafe.Sizeof(uv.Cell{}))

func (s *Scrollback) SetMaxBytes(limit int) {
	if s == nil || limit < 0 {
		return
	}
	s.byteLimited = true
	s.maxBytes = limit
	s.maxLines = max(1, limit/scrollbackRowBytes)
	// This runs only on configuration changes; output uses saved row sizes.
	if len(s.rowBytes) != len(s.lines) {
		s.rowBytes = make([]int, len(s.lines))
		s.packed = make([][]byte, len(s.lines))
		s.styled = make([]*styledRow, len(s.lines))
	}
	s.bytes = 0
	if s.indexed != nil {
		s.bytes = s.indexed.view.bytes()
	}
	for i := 0; i < s.count; i++ {
		idx := s.wrap(s.start + i)
		width := cap(s.lines[idx])
		if s.packed[idx] != nil {
			width = len(s.packed[idx])
		}
		if s.styled[idx] != nil {
			width = len(s.styled[idx].glyphs)
		}
		s.rowBytes[idx] = scrollbackRowBytes + width*scrollbackCellBytes
		s.bytes += s.rowBytes[idx]
	}
	for s.Len() > 0 && (s.bytes > limit || s.Len() > s.maxLines) {
		s.dropOldest()
	}
}
func (s *Scrollback) ByteLimited() bool { return s != nil && s.byteLimited }
func (s *Scrollback) Bytes() int {
	if s == nil {
		return 0
	}
	return s.bytes
}
func (s *Scrollback) dropOldest() (uv.Line, []byte, *styledRow) {
	if s.Len() == 0 {
		return nil, nil, nil
	}
	if s.indexedLen() > 0 {
		charge := s.indexed.dropRows(1)
		if s.byteLimited {
			s.bytes -= charge
		}
		if s.OnDrop != nil {
			s.OnDrop()
		}
		return nil, nil, nil
	}
	idx := s.start
	row := s.lines[idx]
	var text []byte
	var styled *styledRow
	if len(s.styled) > idx {
		text, styled = s.packed[idx], s.styled[idx]
		s.styled[idx], s.packed[idx] = nil, nil
	}
	s.lines[idx] = nil
	if s.byteLimited {
		s.bytes -= s.rowBytes[idx]
		s.rowBytes[idx] = 0
	}
	s.start = s.wrap(s.start + 1)
	s.count--
	if s.OnDrop != nil {
		s.OnDrop()
	}
	return row, text, styled
}
func (s *Scrollback) pushBytes(line uv.Line) bool {
	return s.pushBytesWithPlain(line, false)
}

func (s *Scrollback) pushBytesWithPlain(line uv.Line, knownPlain bool) bool {
	return s.pushBytesWithPlainText(line, knownPlain, nil)
}

func (s *Scrollback) pushBytesWithPlainText(line uv.Line, knownPlain bool, plainText []byte) (dropped bool) {
	cost := scrollbackRowBytes + len(line)*scrollbackCellBytes
	var reuse uv.Line
	var text []byte
	var styled *styledRow
	for s.Len() > 0 && (s.bytes+cost > s.maxBytes || s.Len() >= s.maxLines) {
		dropped = dropped || s.OnDrop != nil
		reuse, text, styled = s.dropOldest()
	}
	if cost > s.maxBytes {
		if s.OnDrop != nil {
			dropped = true
			s.OnDrop()
		}
		return dropped
	}
	if s.count == len(s.lines) {
		n := min(s.maxLines, max(1, 2*len(s.lines)))
		rows := make([]uv.Line, n)
		sizes := make([]int, n)
		packed := make([][]byte, n)
		styles := make([]*styledRow, n)
		for i := 0; i < s.count; i++ {
			idx := s.wrap(s.start + i)
			rows[i] = s.lines[idx]
			sizes[i] = s.rowBytes[idx]
			packed[i] = s.packed[idx]
			styles[i] = s.styled[idx]
		}
		s.lines, s.rowBytes, s.packed, s.styled, s.start = rows, sizes, packed, styles, 0
	}
	idx := s.wrap(s.start + s.count)
	reusable := cap(text) == len(line)
	plain := knownPlain && len(line) > 0
	if plain {
		if reusable {
			text = text[:len(line)]
			if plainText != nil && len(plainText) == len(line) {
				copy(text, plainText)
			} else {
				for x := range line {
					text[x] = line[x].Content[0]
				}
			}
		}
	} else if reusable {
		// text belongs to the evicted row; a rejected prefix is never retained.
		text = text[:len(line)]
		plain = packPlainRow(line, text)
	} else {
		plain = plainRow(line)
	}
	if plain {
		if !reusable {
			text = make([]byte, len(line))
			if plainText != nil && len(plainText) == len(line) {
				copy(text, plainText)
			} else {
				for x := range line {
					text[x] = line[x].Content[0]
				}
			}
		}
		s.packed[idx], s.lines[idx] = text, nil
	} else if styled = packStyled(line, styled); styled != nil {
		s.styled[idx], s.lines[idx], s.packed[idx] = styled, nil, nil
	} else {
		// Exact capacity prevents an evicted wider row retaining hidden storage.
		if cap(reuse) == len(line) {
			reuse = append(reuse[:0], line...)
		} else {
			reuse = make(uv.Line, len(line))
			copy(reuse, line)
		}
		s.lines[idx], s.packed[idx] = reuse, nil
	}
	s.rowBytes[idx] = cost
	s.bytes += cost
	s.count++
	return dropped
}
