package server

import (
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMailDeliveryAndAcknowledgementReservePaneID(t *testing.T) {
	for _, phase := range []string{"delivery", "acknowledgement"} {
		t.Run(phase, func(t *testing.T) {
			teststate.Lock(t)
			teststate.Setenv(t, "SHELL", "/bin/sh")
			teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
			s := newTestServer(t, "mail-reuse")
			s.mailCh = make(chan mailWrite, 4)
			s.mailInFlight = map[int64]bool{}
			s.jobInFlight = map[string]bool{}
			s.app.State().Apply(model.CmdSplitFocused{Dir: layout.Vertical})
			f := s.app.State().Snapshot()
			f.NextPaneID = 9
			s.app.RestoreSession(f)
			t.Cleanup(func() {
				for _, id := range []int{1, 2, 3, 10} {
					s.sup.Kill(id)
				}
			})
			store, err := s.db()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.store.Close() })
			s.app.State().SetPaneReservations(func() (map[int]bool, error) { return s.store.ReservedPaneIDs(s.session) })
			s.app.SetReported(2, "codex", "working")
			response := s.msgSend("send", api.MsgSendParams{Room: "room", FromAgent: "sender", To: "p2", Body: "# old mail"})
			if response.Err != nil {
				t.Fatal(response.Err)
			}
			if len(s.mailCh) != 1 {
				t.Fatal("delivery was not queued")
			}
			w := <-s.mailCh
			if !s.mailInFlightSet()[w.msgID] {
				t.Fatal("message is not in flight")
			}

			type deliveryResult struct {
				written bool
				err     error
			}
			var completed chan deliveryResult
			locked := false
			if phase == "delivery" {
				s.mailDelivery.Lock()
				locked = true
				started := make(chan struct{})
				completed = make(chan deliveryResult, 1)
				go func() {
					close(started)
					written, err := s.deliverMailWrite(w)
					completed <- deliveryResult{written, err}
				}()
				<-started
				defer func() {
					if locked {
						s.mailDelivery.Unlock()
						<-completed
					}
				}()
			} else {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				readOnly, err := dispatch.OpenReadOnly(dispatch.DBPath(s.session))
				if err != nil {
					t.Fatal(err)
				}
				s.store = readOnly
				written, err := s.deliverMailWrite(w)
				if !written || err == nil || len(s.mailAwaitingAck) != 1 {
					t.Fatalf("write=%v err=%v awaiting=%d", written, err, len(s.mailAwaitingAck))
				}
			}
			pending, err := s.store.PendingForAgentCreationOrder("p2")
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending=%v err=%v", pending, err)
			}
			s.app.ExecCmd(model.CmdClosePane{Pane: 2})
			s.app.ExecCmd(model.CmdSplitFocused{Dir: layout.Vertical})
			if got := s.app.State().FocusedPane(); got != 3 {
				t.Fatalf("new pane = p%d, want smallest unreserved p3 while p2 is %s", got, phase)
			}
			if phase == "delivery" {
				s.mailDelivery.Unlock()
				locked = false
				result := <-completed
				if result.written || result.err == nil {
					t.Fatalf("old write reached replacement: %+v", result)
				}
			} else {
				if err := s.store.Close(); err != nil {
					t.Fatal(err)
				}
				s.store, err = dispatch.Open(dispatch.DBPath(s.session))
				if err != nil {
					t.Fatal(err)
				}
				s.mailDelivery.Lock()
				err = s.retryMailAcknowledgementsLocked()
				s.mailDelivery.Unlock()
				if err != nil || len(s.mailAwaitingAck) != 0 || s.mailInFlightSet()[w.msgID] {
					t.Fatalf("acknowledgement retry err=%v", err)
				}
				s.app.ExecCmd(model.CmdClosePane{Pane: 3})
				s.app.ExecCmd(model.CmdSplitFocused{Dir: layout.Vertical})
				if got := s.app.State().FocusedPane(); got != 2 {
					t.Fatalf("released pane = p%d, want p2", got)
				}
			}
		})
	}
}
