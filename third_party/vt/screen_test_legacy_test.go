//go:build !flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

func screenTestLines(b *screenBuffer) []uv.Line { return b.Lines }

func screenTestRaw(s *Screen, x, y int, c uv.Cell) { s.buf.Lines[y][x] = c }
func screenTestNilRow(s *Screen, y int)            { s.buf.Lines[y] = nil }
