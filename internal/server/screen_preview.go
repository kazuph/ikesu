package server

import (
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/screenprompt"
)

type screenPreviewScan struct {
	original      int
	expected      int
	visited       map[string]*string
	lastStep      time.Time
	lastDirection string
}

func (s *Server) startPreviewScan(pid int, w *screenPromptWatch) {
	p := w.prompt
	w.preview = &screenPreviewScan{original: p.Cursor, expected: -1, visited: map[string]*string{}}
	if p.Cursor < 0 || p.Cursor >= len(p.Items) {
		s.finishPreviewScan(pid, w)
		return
	}
	s.stepPreviewScan(pid, w, p)
}

func (s *Server) previewArrow(pid int, w *screenPromptWatch, current, target int) bool {
	if current == target {
		return true
	}
	arrow := "\x1b[B"
	next := current + 1
	if target < current {
		arrow = "\x1b[A"
		next = current - 1
	}
	pane := s.sup.Get(pid)
	if pane == nil {
		return false
	}
	if _, err := pane.WriteToPTY([]byte(arrow)); err != nil {
		return false
	}
	w.preview.expected = next
	w.preview.lastDirection = arrow
	w.preview.lastStep = time.Now()
	return true
}

func (s *Server) stepPreviewScan(pid int, w *screenPromptWatch, p *screenprompt.Prompt) {
	scan := w.preview
	if !scan.lastStep.IsZero() && time.Since(scan.lastStep) < agentPromptSubmitDelay {
		return
	}
	if p.Cursor < 0 || p.Cursor >= len(p.Items) || scan.expected >= 0 && p.Cursor != scan.expected {
		s.restorePreviewCursor(pid, w)
		s.finishPreviewScan(pid, w)
		return
	}
	scan.expected = -1
	if p.HasPreview && p.Preview == nil {
		s.restorePreviewCursor(pid, w)
		s.finishPreviewScan(pid, w)
		return
	}
	for _, o := range p.Params.Options {
		if o.ID == p.Items[p.Cursor] {
			scan.visited[o.ID] = p.Preview
		}
	}
	for _, o := range p.Params.Options {
		if _, visited := scan.visited[o.ID]; visited {
			continue
		}
		target := -1
		for i, id := range p.Items {
			if id == o.ID {
				target = i
			}
		}
		if target < 0 || !s.previewArrow(pid, w, p.Cursor, target) {
			s.restorePreviewCursor(pid, w)
			s.finishPreviewScan(pid, w)
		}
		return
	}
	if p.Cursor != scan.original {
		if !s.previewArrow(pid, w, p.Cursor, scan.original) {
			s.restorePreviewCursor(pid, w)
			s.finishPreviewScan(pid, w)
		}
		return
	}
	s.finishPreviewScan(pid, w)
}

// Best-effort restoration uses only arrow keys, even on disappearance or reload.
func (s *Server) restorePreviewCursor(pid int, w *screenPromptWatch) {
	scan := w.preview
	if scan == nil {
		return
	}
	pane := s.sup.Get(pid)
	if pane == nil {
		return
	}
	p := s.readScreenPrompt(pid)
	if p != nil && p.Signature() == w.signature && p.Cursor >= 0 && scan.original >= 0 {
		delta := scan.original - p.Cursor
		arrow := "\x1b[B"
		if delta < 0 {
			arrow = "\x1b[A"
			delta = -delta
		}
		if delta > 0 {
			_, _ = pane.WriteToPTY([]byte(strings.Repeat(arrow, delta)))
		}
	} else if scan.lastDirection != "" {
		arrow := "\x1b[A"
		if scan.lastDirection == arrow {
			arrow = "\x1b[B"
		}
		_, _ = pane.WriteToPTY([]byte(arrow))
	}
}

func (s *Server) finishPreviewScan(pid int, w *screenPromptWatch) {
	s.cachePreviewScan(w)
	s.createScreenPrompt(pid, w)
}

func (s *Server) cachePreviewScan(w *screenPromptWatch) {
	w.previews = w.preview.visited
	w.preview = nil
	s.applyCapturedPreviews(w)
}

func (s *Server) applyCapturedPreviews(w *screenPromptWatch) {
	if w.previews == nil {
		return
	}
	for i := range w.prompt.Params.Options {
		o := &w.prompt.Params.Options[i]
		value, visited := w.previews[o.ID]
		o.Preview = value
		o.PreviewUnavailable = !visited
	}
}
