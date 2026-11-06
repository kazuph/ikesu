package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestLateDeliveryRejectsControlBeforeTerminalWrite(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-control")
	late := &lateCodexQuestion{pid: 999, delivery: &api.DecisionDelivery{Status: "pending", Text: "日本語\x1b[201~\r危険"}}
	s.screenPrompts.late = map[string]*lateCodexQuestion{"dec-control": late}
	s.scanLateCodexDeliveries()
	if late.sent || late.delivery.Status != "failed" || late.delivery.Reason != "回答に送信できない制御文字が含まれています" {
		t.Fatalf("delivery=%+v sent=%v", late.delivery, late.sent)
	}
}

func TestLateDeliveryPasteFraming(t *testing.T) {
	unsafe := "日本語\x1b[201~\r予期しない入力"
	if safeLateAnswer(unsafe) {
		t.Fatal("paste terminator was accepted")
	}
	text := "日本語の回答\n二行目"
	if !safeLateAnswer(text) {
		t.Fatal("multiline answer was rejected")
	}
	var framer input.Framer
	events := framer.Push([]byte("\x1b[200~" + text + "\x1b[201~\r"))
	if len(events) != 3 || events[0].Kind != input.KindUnknown || events[1].Kind != input.KindPaste || events[1].Text != text || events[2].Kind != input.KindKey {
		t.Fatalf("events=%+v", events)
	}
}
