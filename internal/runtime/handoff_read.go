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
	UTF8, Partial                                           []byte
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
	history, count, start := r.history, r.historyCount, r.start
	if r.historyIndexed {
		history = make([]rowRef, r.historyLen())
		for y := range history {
			history[y] = r.historyRef(y)
		}
		count, start = len(history), 0
	}
	return &readTransfer{Main: transferRows(r.main), Alternate: transferRows(r.alternate), History: transferRows(history), AltHistory: transferRows(r.altHistory), ByteHistory: r.byteHistory, Pending: r.pending, HistoryCount: count, Start: start, AltStart: r.altStart, Top: r.top, Bottom: r.bottom, Left: r.left, Right: r.right, State: r.state, Kind: r.kind, UTF8: r.utf8Pending, Partial: r.partial}
}
func (r *readTransfer) restore() *readBuffer {
	if r == nil {
		return nil
	}
	parser := ansi.NewParser()
	if len(r.Partial) > 0 {
		_, _, _, _ = ansi.DecodeSequence(r.Partial, ansi.NormalState, parser)
	}
	return &readBuffer{main: restoreRows(r.Main), alternate: restoreRows(r.Alternate), history: restoreRows(r.History), altHistory: restoreRows(r.AltHistory), byteHistory: r.ByteHistory, pending: r.Pending, historyCount: r.HistoryCount, start: r.Start, altStart: r.AltStart, top: r.Top, bottom: r.Bottom, left: r.Left, right: r.Right, state: r.State, kind: r.Kind, utf8Pending: r.UTF8, partial: r.Partial, parser: parser}
}
