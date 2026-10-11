package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"unsafe"
)

// Byte limits account for the row slice and its owned cell backing storage.
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
	}
	s.bytes = 0
	for i := 0; i < s.count; i++ {
		idx := s.wrap(s.start + i)
		s.rowBytes[idx] = scrollbackRowBytes + cap(s.lines[idx])*scrollbackCellBytes
		s.bytes += s.rowBytes[idx]
	}
	for s.count > 0 && (s.bytes > limit || s.count > s.maxLines) {
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
func (s *Scrollback) dropOldest() uv.Line {
	if s.count == 0 {
		return nil
	}
	idx := s.start
	row := s.lines[idx]
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
	return row
}
func (s *Scrollback) pushBytes(line uv.Line) {
	cost := scrollbackRowBytes + len(line)*scrollbackCellBytes
	var reuse uv.Line
	for s.count > 0 && (s.bytes+cost > s.maxBytes || s.count >= s.maxLines) {
		reuse = s.dropOldest()
	}
	if cost > s.maxBytes {
		if s.OnDrop != nil {
			s.OnDrop()
		}
		return
	}
	if s.count == len(s.lines) {
		n := min(s.maxLines, max(1, 2*len(s.lines)))
		rows := make([]uv.Line, n)
		sizes := make([]int, n)
		for i := 0; i < s.count; i++ {
			idx := s.wrap(s.start + i)
			rows[i] = s.lines[idx]
			sizes[i] = s.rowBytes[idx]
		}
		s.lines, s.rowBytes, s.start = rows, sizes, 0
	}
	// Reuse only an exactly sized allocation: hidden capacity also consumes
	// the configured byte budget and must not survive a shorter replacement.
	if cap(reuse) == len(line) {
		reuse = append(reuse[:0], line...)
	} else {
		reuse = make(uv.Line, len(line))
		copy(reuse, line)
	}
	idx := s.wrap(s.start + s.count)
	s.lines[idx] = reuse
	s.rowBytes[idx] = cost
	s.bytes += cost
	s.count++
}
