package server

import (
	"github.com/kazuph/ikesu/internal/api"

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
