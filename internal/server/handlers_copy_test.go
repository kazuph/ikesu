package server

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

// copyMotion issues pane.copy_motion and expects success.
func copyMotion(t *testing.T, dir, paneID string, row uint32, col uint16, motion string) api.PaneCopyMotionResult {
	t.Helper()
	var res api.PaneCopyMotionResult
	apiOK(t, dir, api.MethodPaneCopyMotion, api.PaneCopyMotionParams{
		PaneID: paneID,
		Cursor: api.PaneTextPoint{Row: row, Col: col},
		Motion: motion,
	}, &res)
	return res
}

// copySearch issues pane.copy_search with the pane's live revision.
func copySearch(t *testing.T, dir, paneID, query, dirn string, row uint32, col uint16) api.PaneCopySearchResult {
	t.Helper()
	var rd api.PaneReadResultEnvelope
	apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: paneID}, &rd)
	var res api.PaneCopySearchResult
	apiOK(t, dir, api.MethodPaneCopySearch, api.PaneCopySearchParams{
		PaneID:          paneID,
		Query:           query,
		Direction:       dirn,
		Cursor:          api.PaneTextPoint{Row: row, Col: col},
		ContentRevision: rd.Read.Revision,
	}, &res)
	return res
}

// SPEC G6: the copy_motion/copy_search API surface — motions, search
// shape, case sensitivity, wrapping, and error contract — verified
// against the observed herdr behavior.
func TestPaneCopyAPI(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	p1 := firstPaneID(t, dir)
	// QMARK_ok only exists in the printf output, not the command echo.
	// Keep the buffer unchanged while the revision-bound requests run.
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: p1,
		Text:   "clear; printf 'foo bar.baz qux\\nhello   world\\nQMARK_%s\\n' ok; exec /bin/cat",
		Keys:   []string{"enter"},
	}, nil)
	waitRead(t, dir, p1, "QMARK_ok")

	// Locate the marker rows through search (row numbers vary with the
	// prompt layout).
	fwd := copySearch(t, dir, p1, "bar.baz", "forward", 0, 0)
	if fwd.Type != "pane_copy_search" {
		t.Fatalf("type = %q", fwd.Type)
	}
	if fwd.Total < 1 || fwd.Current == nil {
		t.Fatalf("forward search = %+v", fwd)
	}
	// The last match is the printf output line (the earlier match is
	// the shell's command echo).
	out := fwd.Matches[fwd.Total-1]
	row := out.Start.Row
	if got := out.Start.Col; got != 4 {
		t.Fatalf("bar.baz col = %d, want 4", got)
	}
	if out.End.Col != 10 {
		t.Fatalf("match end col = %d, want 10", out.End.Col)
	}

	// line_end lands on the last real cell ("qux" ends at col 14).
	if res := copyMotion(t, dir, p1, row, 0, "line_end"); res.Cursor.Col != 14 {
		t.Fatalf("line_end col = %d, want 14", res.Cursor.Col)
	}
	// Word motions (vi-class: '.' is its own word).
	if res := copyMotion(t, dir, p1, row, 0, "next_word_start"); res.Cursor.Col != 4 {
		t.Fatalf("next_word_start = %d, want 4", res.Cursor.Col)
	}
	if res := copyMotion(t, dir, p1, row, 6, "next_word_start"); res.Cursor.Col != 7 {
		t.Fatalf("next_word_start from '.' area = %d, want 7", res.Cursor.Col)
	}
	if res := copyMotion(t, dir, p1, row, 12, "previous_word_start"); res.Cursor.Col != 8 {
		t.Fatalf("previous_word_start = %d, want 8", res.Cursor.Col)
	}
	if res := copyMotion(t, dir, p1, row, 6, "next_big_word_start"); res.Cursor.Col != 12 {
		t.Fatalf("next_big_word_start = %d, want 12", res.Cursor.Col)
	}
	if res := copyMotion(t, dir, p1, row, 0, "first_non_blank"); res.Cursor.Col != 0 {
		t.Fatalf("first_non_blank = %d, want 0", res.Cursor.Col)
	}

	// Search is case-sensitive.
	if res := copySearch(t, dir, p1, "BAR.BAZ", "forward", 0, 0); res.Total != 0 {
		t.Fatalf("case-sensitive search total = %d, want 0", res.Total)
	}
	// Backward search wraps to the last match.
	bwd := copySearch(t, dir, p1, "qux", "backward", 0, 0)
	if bwd.Current == nil || uint64(*bwd.Current) != bwd.Total-1 {
		t.Fatalf("backward wrap current = %+v", bwd)
	}
	// Invalid direction → invalid_request.
	var rd api.PaneReadResultEnvelope
	apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: p1}, &rd)
	resp := apiReq(t, dir, api.MethodPaneCopySearch, api.PaneCopySearchParams{
		PaneID: p1, Query: "x", Direction: "sideways",
		Cursor: api.PaneTextPoint{Row: 0, Col: 0}, ContentRevision: rd.Read.Revision,
	})
	if resp.Err == nil || resp.Err.Code != api.ErrInvalidRequest {
		t.Fatalf("bad direction: %+v", resp.Err)
	}
	// Invalid motion → invalid_request.
	resp = apiReq(t, dir, api.MethodPaneCopyMotion, api.PaneCopyMotionParams{
		PaneID: p1, Motion: "teleport", Cursor: api.PaneTextPoint{Row: 0, Col: 0},
	})
	if resp.Err == nil || resp.Err.Code != api.ErrInvalidRequest {
		t.Fatalf("bad motion: %+v", resp.Err)
	}
	// Out-of-range row → copy_motion_unavailable.
	resp = apiReq(t, dir, api.MethodPaneCopyMotion, api.PaneCopyMotionParams{
		PaneID: p1, Motion: "line_end", Cursor: api.PaneTextPoint{Row: 99999, Col: 0},
	})
	if resp.Err == nil || resp.Err.Code != api.ErrCopyMotion {
		t.Fatalf("out-of-range row: %+v", resp.Err)
	}
	// Stale revision beats every other check.
	resp = apiReq(t, dir, api.MethodPaneCopyMotion, api.PaneCopyMotionParams{
		PaneID: p1, Motion: "teleport", Cursor: api.PaneTextPoint{Row: 99999, Col: 0},
		ContentRevision: ptrU64(rd.Read.Revision + 99),
	})
	if resp.Err == nil || resp.Err.Code != api.ErrStaleContent {
		t.Fatalf("stale revision: %+v", resp.Err)
	}
}

func ptrU64(v uint64) *uint64 { return &v }
