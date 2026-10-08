package server

// pane.copy_motion / pane.copy_search — the retained-buffer copy
// contract (SPEC G6 / herdr-api-schema). Buffer coordinates are
// absolute: scrollback rows first, then the screen.

import (
	"fmt"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/copybuf"
)

func (s *Server) paneCopyMotion(id string, p api.PaneCopyMotionParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	lines := pane.BufferLines()
	rev := pane.Snapshot().Revision
	// stale_content is checked before the cursor row (observed order).
	if p.ContentRevision != nil && *p.ContentRevision != rev {
		return api.Failure(id, api.ErrStaleContent, "pane content changed")
	}
	if p.Cursor.Row >= uint32(len(lines)) {
		return api.Failure(id, api.ErrCopyMotion, "terminal row is unavailable")
	}
	valid := false
	for _, m := range copybuf.Motions {
		if p.Motion == m {
			valid = true
			break
		}
	}
	if !valid {
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid request: unknown variant `%s`, expected one of `%s`",
				p.Motion, strings.Join(copybuf.Motions, "`, `")))
	}
	to, _ := copybuf.Motion(lines, p.Motion, int(p.Cursor.Row), int(p.Cursor.Col))
	return api.Success(id, api.PaneCopyMotionResult{
		Type:            "pane_copy_motion",
		PaneID:          fmtPaneID(pid),
		Cursor:          api.PaneTextPoint{Row: uint32(to.Row), Col: uint16(to.Col)},
		ContentRevision: rev,
	})
}

func (s *Server) paneCopySearch(id string, p api.PaneCopySearchParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	lines := pane.BufferLines()
	rev := pane.Snapshot().Revision
	if p.ContentRevision != rev {
		return api.Failure(id, api.ErrStaleContent, "pane content changed")
	}
	if p.Direction != "forward" && p.Direction != "backward" {
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid request: unknown variant `%s`, expected `forward` or `backward`", p.Direction))
	}
	found := copybuf.Search(lines, p.Query)
	matches := make([]api.PaneTextRange, len(found))
	for i, m := range found {
		matches[i] = api.PaneTextRange{
			Start: api.PaneTextPoint{Row: uint32(m.Start.Row), Col: uint16(m.Start.Col)},
			End:   api.PaneTextPoint{Row: uint32(m.End.Row), Col: uint16(m.End.Col)},
		}
	}
	res := api.PaneCopySearchResult{
		Type:            "pane_copy_search",
		PaneID:          fmtPaneID(pid),
		ContentRevision: rev,
		Matches:         matches,
		Total:           uint64(len(matches)),
	}
	// `previous`, when present, replaces the cursor as the anchor
	// (observed: the range's start is used).
	anchor := copybuf.Point{Row: int(p.Cursor.Row), Col: int(p.Cursor.Col)}
	if p.Previous != nil {
		anchor = copybuf.Point{Row: int(p.Previous.Start.Row), Col: int(p.Previous.Start.Col)}
	}
	if idx := copybuf.CurrentIndex(found, anchor, p.Direction == "forward"); idx >= 0 {
		cur := uint32(idx)
		res.Current = &cur
		cg := uint64(idx)
		res.CurrentGlobal = &cg
	}
	return api.Success(id, res)
}
