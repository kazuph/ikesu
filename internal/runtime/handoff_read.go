package runtime

import "github.com/charmbracelet/x/ansi"

type rowTransfer struct {
	Continued bool
	Width     int
}
type readTransfer struct {
	Main, Alternate, History, AltHistory                    []rowTransfer
	ByteHistory, Pending                                    bool
	HistoryCount, Start, AltStart, Top, Bottom, Left, Right int
	State, Kind                                             byte
	UTF8                                                    []byte
}

func transferRows(rows []rowRef) []rowTransfer {
	out := make([]rowTransfer, len(rows))
	for i, r := range rows {
		out[i] = rowTransfer{r.continued, r.width}
	}
	return out
}
func restoreRows(rows []rowTransfer) []rowRef {
	out := make([]rowRef, len(rows))
	for i, r := range rows {
		out[i] = rowRef{r.Continued, r.Width}
	}
	return out
}
func (r *readBuffer) transfer() *readTransfer {
	if r == nil {
		return nil
	}
	return &readTransfer{Main: transferRows(r.main), Alternate: transferRows(r.alternate), History: transferRows(r.history), AltHistory: transferRows(r.altHistory), ByteHistory: r.byteHistory, Pending: r.pending, HistoryCount: r.historyCount, Start: r.start, AltStart: r.altStart, Top: r.top, Bottom: r.bottom, Left: r.left, Right: r.right, State: r.state, Kind: r.kind, UTF8: r.utf8Pending}
}
func (r *readTransfer) restore() *readBuffer {
	if r == nil {
		return nil
	}
	return &readBuffer{main: restoreRows(r.Main), alternate: restoreRows(r.Alternate), history: restoreRows(r.History), altHistory: restoreRows(r.AltHistory), byteHistory: r.ByteHistory, pending: r.Pending, historyCount: r.HistoryCount, start: r.Start, altStart: r.AltStart, top: r.Top, bottom: r.Bottom, left: r.Left, right: r.Right, state: r.State, kind: r.Kind, utf8Pending: r.UTF8, parser: ansi.NewParser()}
}
