package server

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// Durable mail is persisted by global pane ID and acknowledged after PTY writes.
// The jobs room batches notifications; ordinary mail submits each body in order.

// mailWrite is one unit of work for the serialized delivery worker.
type mailWrite struct {
	pane    int
	payload []byte
	// regular mail: mark this dispatch delivered on write completion.
	msgID int64
	// job mail batch: mark exactly the message ids whose bodies were
	// written. Marking by room+recipient would race a completion that
	// arrives between payload assembly and write completion — it would
	// be flagged delivered without ever reaching the pane.
	msgIDs  []int64
	room    string
	toAgent string
}

// mailWorker serializes all mailbox PTY writes so message order and
// write-completion tracking stay consistent even when several API
// requests flush the same pane concurrently.
func (s *Server) mailWorker() {
	tick := time.NewTicker(jobActiveRefresh)
	defer tick.Stop()
	for {
		select {
		case w, ok := <-s.mailCh:
			if !ok {
				return
			}
			written, err := s.deliverMailWrite(w)
			if written && err != nil {
				fmt.Fprintf(os.Stderr, "ikesu mail acknowledgement: %v\n", err)
				continue
			}
			if err != nil {
				s.failMissingJobWrite(w)
			}
			s.mailDone(w)
		case <-tick.C:
			s.mailDelivery.Lock()
			hadPending := len(s.mailAwaitingAck) > 0
			err := s.retryMailAcknowledgementsLocked()
			s.mailDelivery.Unlock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "ikesu mail acknowledgement: %v\n", err)
			}
			if hadPending && err == nil {
				s.app.Do(s.flushAllMail)
			}
		}
	}
}

// No app-loop callback is allowed while holding the delivery fence: handoff
// acquires it on that loop. Failure handling runs after this method returns.
func (s *Server) deliverMailWrite(w mailWrite) (bool, error) {
	s.mailDelivery.Lock()
	defer s.mailDelivery.Unlock()
	if s.closing.Load() == 2 {
		return false, nil
	} // importer owns the still-queued notice
	p := s.sup.Get(w.pane)
	if p == nil {
		return false, fmt.Errorf("mail destination missing")
	}
	p.RecordExternalInput()
	if _, err := p.WriteToPTY(w.payload); err != nil {
		return false, err
	}
	if err := s.acknowledgeMailWrite(w); err != nil {
		s.mailAwaitingAck = append(s.mailAwaitingAck, w)
		return true, err
	}
	return true, nil
}

func (s *Server) acknowledgeMailWrite(w mailWrite) error {
	store, err := s.db()
	if err != nil {
		return err
	}
	if w.msgID != 0 {
		_, err = store.MarkMessageDelivered(w.msgID)
	} else if len(w.msgIDs) > 0 {
		_, err = store.MarkDeliveredByIDs(w.msgIDs)
	}
	return err
}

// Caller holds mailDelivery. Pending writes retain their in-flight keys so
// neither a periodic flush nor another batch may write their payload again.
func (s *Server) retryMailAcknowledgementsLocked() error {
	var pending []mailWrite
	var failures []error
	for _, w := range s.mailAwaitingAck {
		if err := s.acknowledgeMailWrite(w); err != nil {
			pending = append(pending, w)
			failures = append(failures, err)
		} else {
			s.mailDone(w)
		}
	}
	s.mailAwaitingAck = pending
	return errors.Join(failures...)
}

func jobKey(room, toAgent string) string { return room + "\x00" + toAgent }

// mailDone releases the in-flight markers of a completed write.
func (s *Server) mailDone(w mailWrite) {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	if w.msgID != 0 {
		delete(s.mailInFlight, w.msgID)
	}
	if w.room != "" {
		delete(s.jobInFlight, jobKey(w.room, w.toAgent))
	}
}

// enqueueMail stages one message or job batch for delivery. It returns
// false when the delivery queue is full — the message stays queued and a
// later flush retries it.
func (s *Server) enqueueMail(w mailWrite) bool {
	s.mailMu.Lock()
	if w.msgID != 0 {
		if s.mailInFlight[w.msgID] {
			s.mailMu.Unlock()
			return true // already staged
		}
		s.mailInFlight[w.msgID] = true
	}
	if w.room != "" {
		k := jobKey(w.room, w.toAgent)
		if s.jobInFlight[k] {
			s.mailMu.Unlock()
			return true // batch for this recipient already staged
		}
		s.jobInFlight[k] = true
	}
	s.mailMu.Unlock()
	select {
	case s.mailCh <- w:
		return true
	default:
		s.mailDone(w)
		return false
	}
}

func (s *Server) mailInFlightSet() map[int64]bool {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	out := make(map[int64]bool, len(s.mailInFlight))
	for id := range s.mailInFlight {
		out[id] = true
	}
	return out
}

// db lazily opens the shared dispatch store; the error is cached.
func (s *Server) db() (*dispatch.Store, error) {
	s.dbOnce.Do(func() {
		s.store, s.dbErr = dispatch.Open(dispatch.DBPath(s.session))
	})
	return s.store, s.dbErr
}

// --- target resolution ---

// paneTarget is a resolved pane inside the model.
type paneTarget struct {
	pid, wsIdx, tabIdx int
}

// isAgentPane reports whether the pane has a detected or reported agent
// identity (mirrors herdr's is_agent_terminal).
func (s *Server) isAgentPane(pid int) bool {
	if rep, _, ok := s.app.Reported(pid); ok && rep != "" {
		return true
	}
	if p := s.sup.Get(pid); p != nil && p.Snapshot().AgentID != "" {
		return true
	}
	return false
}

// agentNameOf returns the pane's user-facing agent name: the manual label
// when set (pane rename / agent rename), else the detected/reported agent
// kind (herdr's effective_agent_label).
func (s *Server) agentNameOf(pid int) string {
	if label := s.app.State().PaneLabels[pid]; label != "" {
		return label
	}
	if rep, _, ok := s.app.Reported(pid); ok && rep != "" {
		return rep
	}
	if p := s.sup.Get(pid); p != nil {
		return p.Snapshot().AgentID
	}
	return ""
}

// mailboxName is the durable recipient for the jobs room: the pane's
// label if any, else its pane id when it hosts an agent. A pane with no
// reported agent identity has no mailbox.
func (s *Server) mailboxName(pid int) (string, bool) {
	if label := s.app.State().PaneLabels[pid]; label != "" {
		return label, true
	}
	if s.isAgentPane(pid) {
		return fmtPaneID(pid), true
	}
	return "", false
}

// eachPane iterates live panes with their model location.
func (s *Server) eachPane(fn func(pid, wsIdx, tabIdx int)) {
	st := s.app.State()
	for i, ws := range st.Workspaces {
		for j, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				fn(pid, i, j)
			}
		}
	}
}

// resolveTarget maps a user target (pane id or agent/label name) to a
// pane. agentOnly mirrors resolve_agent_target: pane ids must point at an
// agent pane; name matches consider every pane's label/agent name. The
// lookup fails closed on not-found and ambiguity.
func (s *Server) resolveTarget(target string, agentOnly bool) (paneTarget, *api.Error) {
	target = strings.TrimSpace(target)
	if n, ok := parsePaneID(target); ok {
		wsIdx, tabIdx := s.app.State().FindPane(n)
		if wsIdx >= 0 {
			if agentOnly && !s.isAgentPane(n) {
				return paneTarget{}, &api.Error{Code: api.ErrAgentNotFound,
					Message: fmt.Sprintf("agent target %s is not an agent pane", target)}
			}
			return paneTarget{pid: n, wsIdx: wsIdx, tabIdx: tabIdx}, nil
		}
		return paneTarget{}, &api.Error{Code: api.ErrAgentNotFound, Message: fmt.Sprintf("pane target not found: %s", target)}
	}
	var matches []paneTarget
	s.eachPane(func(pid, wsIdx, tabIdx int) {
		if s.agentNameOf(pid) == target {
			matches = append(matches, paneTarget{pid, wsIdx, tabIdx})
		}
	})
	switch len(matches) {
	case 0:
		return paneTarget{}, &api.Error{Code: api.ErrAgentNotFound,
			Message: fmt.Sprintf("agent target not found: %s", target)}
	case 1:
		return matches[0], nil
	default:
		panes := make([]string, len(matches))
		for i, m := range matches {
			panes[i] = fmtPaneID(m.pid)
		}
		return paneTarget{}, &api.Error{Code: api.ErrAmbiguous,
			Message: fmt.Sprintf("agent target %q is ambiguous: %s", target, strings.Join(panes, ", "))}
	}
}

// --- msg handlers ---

func (s *Server) msgSend(id string, p api.MsgSendParams) api.Response {
	room := strings.TrimSpace(p.Room)
	if room == "" {
		return api.Failure(id, api.ErrInvalidMsgRoom, "room must not be empty")
	}
	from := strings.TrimSpace(p.FromAgent)
	if from == "" {
		return api.Failure(id, api.ErrInvalidMsgAgent, "from_agent must not be empty")
	}
	if strings.TrimSpace(p.Body) == "" {
		return api.Failure(id, api.ErrInvalidMsgBody, "message body must not be empty")
	}
	if p.To == "*" {
		return api.Failure(id, api.ErrMsgWildcard,
			"wildcard recipient '*' is not supported; specify one agent or pane target")
	}
	store, derr := s.db()
	if derr != nil {
		return api.Failure(id, api.ErrMsgStore, derr.Error())
	}
	recipient, err := s.resolveMsgRecipient(room, p.To)
	if room == dispatch.JobsRoom && p.FromAgent == dispatch.RunShellActor && !dispatch.IsGlobalPaneID(p.To) {
		err = &api.Error{Code: api.ErrInvalidMsgAgent, Message: "job notification requires immutable global caller pane ID"}
	}
	if err != nil {
		if e := store.InsertFailedMessage(room, p.Project, from, p.To, p.Body, err.Message, p.ReplyTo); e != nil {
			return api.Failure(id, api.ErrMsgStore, e.Error())
		}
		return api.Failure(id, err.Code, err.Message)
	}
	m, derr := store.InsertMessage(room, p.Project, from, recipient, p.Body, p.ReplyTo, false)
	if derr != nil {
		return api.Failure(id, api.ErrMsgStore, derr.Error())
	}
	s.invalidateJobCache()
	var nudged []string
	delivered := false
	if room == dispatch.JobsRoom {
		delivered = s.flushJobNudgeFor(room, recipient)
	} else {
		delivered = s.flushRegularForRecipient(recipient)
	}
	if delivered {
		nudged = []string{recipient}
	}
	return api.Success(id, api.MsgSendResult{
		Type:     "msg_send",
		Messages: []api.MsgMessage{toWireMessage(m)},
		Nudged:   nudged,
	})
}

// resolveMsgRecipient resolves the recipient for a message: the pane id
// for regular rooms, the mailbox name for the jobs room.
func (s *Server) resolveMsgRecipient(room, target string) (string, *api.Error) {
	t, err := s.resolveTarget(target, true)
	if err != nil {
		return "", err
	}

	return fmtPaneID(t.pid), nil
}

// recipientAliases expands an inbox query: the pane id always; for the
// jobs room also the mailbox name when it resolves to the same pane.
func (s *Server) recipientAliases(room, toAgent string) []string {
	t, err := s.resolveTarget(toAgent, false)
	if err != nil {
		return []string{toAgent}
	}
	aliases := []string{fmtPaneID(t.pid)}

	return aliases
}

func (s *Server) msgInbox(id string, p api.MsgInboxParams) api.Response {
	room := strings.TrimSpace(p.Room)
	if room == "" {
		return api.Failure(id, api.ErrInvalidMsgRoom, "room must not be empty")
	}
	toAgent := strings.TrimSpace(p.ToAgent)
	if toAgent == "" {
		return api.Failure(id, api.ErrInvalidMsgAgent, "to_agent must not be empty")
	}
	store, err := s.db()
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	msgs, err := store.MessagesForInboxRecipients(room,
		s.recipientAliases(room, toAgent), s.mailInFlightSet())
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	out := make([]api.MsgMessage, len(msgs))
	for i, m := range msgs {
		out[i] = toWireMessage(m)
	}
	return api.Success(id, api.MsgInboxResult{Type: "msg_inbox", Messages: out})
}

func (s *Server) msgHistory(id string, p api.MsgHistoryParams) api.Response {
	room := strings.TrimSpace(p.Room)
	if room == "" {
		return api.Failure(id, api.ErrInvalidMsgRoom, "room must not be empty")
	}
	store, err := s.db()
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	limit := int(p.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	msgs, err := store.History(room, p.Project, limit)
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	out := make([]api.MsgMessage, len(msgs))
	for i, m := range msgs {
		out[i] = toWireMessage(m)
	}
	return api.Success(id, api.MsgHistoryResult{Type: "msg_history", Messages: out})
}

func (s *Server) msgRooms(id string) api.Response {
	store, err := s.db()
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	rooms, err := store.Rooms()
	if err != nil {
		return api.Failure(id, api.ErrMsgStore, err.Error())
	}
	return api.Success(id, api.MsgRoomsResult{Type: "msg_rooms", Rooms: rooms})
}

func toWireMessage(m dispatch.Message) api.MsgMessage {
	return api.MsgMessage{
		ID: m.ID, Room: m.Room, Project: m.Project,
		FromAgent: m.FromAgent, ToAgent: m.ToAgent, Body: m.Body,
		CreatedAt: m.CreatedAt, DeliveredAt: m.DeliveredAt, ReadAt: m.ReadAt,
	}
}

// --- delivery flushes (run on the app loop) ---

func regularMailCanSubmit(status string) bool {
	return status == "idle" || status == "working" || status == "done"
}

// flushRegularForRecipient flushes a pane-id-addressed recipient after a
// send; it only submits when the pane exists and is receptive.
func (s *Server) flushRegularForRecipient(recipient string) bool {
	t, err := s.resolveTarget(recipient, false)
	if err != nil {
		return false
	}
	if !regularMailCanSubmit(s.app.PaneStatusKey(t.pid)) {
		return false
	}
	return s.flushRegularForPane(t.pid)
}

// flushRegularForPane stages every queued non-jobs-room message addressed
// to the pane id, oldest first. No DB file means nothing is queued; skip
// opening the store so status-transition flushes do not load the engine.
func (s *Server) flushRegularForPane(pid int) bool {
	if !dispatch.DBExists(s.session) {
		return false
	}
	store, err := s.db()
	if err != nil {
		return false
	}
	msgs, err := store.PendingForAgentCreationOrder(fmtPaneID(pid))
	if err != nil || len(msgs) == 0 {
		return false
	}
	delivered := false
	for _, m := range msgs {
		payload := s.encodeMailFor(pid, m.Body)
		if !s.enqueueMail(mailWrite{pane: pid, payload: payload, msgID: m.ID}) {
			return delivered
		}
		delivered = true
	}
	return delivered
}

// flushJobNudgesFor delivers all queued jobs-room nudges for a mailbox
// recipient; job mail reaches working agents too, only blocked panes wait.
// No DB file means no queued nudges; skip opening the store.
func (s *Server) flushJobNudgesFor(toAgent string) bool {
	if !dispatch.DBExists(s.session) {
		return false
	}
	store, err := s.db()
	if err != nil {
		return false
	}
	nudges, err := store.PendingNudgesForAgent(toAgent)
	if err != nil {
		return false
	}
	delivered := false
	for _, n := range nudges {
		if n.Room != dispatch.JobsRoom {
			continue
		}
		if s.flushJobNudge(n) {
			delivered = true
		}
	}
	return delivered
}

// flushJobNudgeFor flushes one room/recipient pair after a send.
func (s *Server) flushJobNudgeFor(room, toAgent string) bool {
	store, err := s.db()
	if err != nil {
		return false
	}
	nudge, err := store.PendingNudgeFor(room, toAgent)
	if err != nil || nudge == nil {
		return false
	}
	return s.flushJobNudge(*nudge)
}

func (s *Server) flushJobNudge(n dispatch.PendingNudge) bool {
	if !dispatch.IsGlobalPaneID(n.ToAgent) {
		s.failJobQueue(n, "job notification requires immutable global caller pane ID")
		return false
	}
	t, err := s.resolveTarget(n.ToAgent, false)
	if err != nil {
		s.failJobQueue(n, err.Message)
		return false
	}
	if s.sup.Get(t.pid) == nil {
		return false
	}
	if s.app.PaneStatusKey(t.pid) == "blocked" {
		return false
	}
	store, derr := s.db()
	if derr != nil {
		return false
	}
	msgs, derr := store.PendingMessagesForAgent(n.Room, n.ToAgent)
	if derr != nil || len(msgs) == 0 {
		return false
	}
	var bodies []string
	var ids []int64
	for _, m := range msgs {
		bodies = append(bodies, m.Body)
		ids = append(ids, m.ID)
	}
	return s.enqueueMail(mailWrite{
		pane: t.pid, payload: s.encodeMailFor(t.pid, strings.Join(bodies, "\n")),
		msgIDs: ids, room: n.Room, toAgent: n.ToAgent,
	})
}

// flushMailForPane flushes regular and job mail for one pane; invoked by
// the app's availability hook on status transitions.
func (s *Server) flushMailForPane(pid int) {
	if regularMailCanSubmit(s.app.PaneStatusKey(pid)) {
		s.flushRegularForPane(pid)
	}
	s.flushJobNudgesFor(fmtPaneID(pid))
}

// flushAllMail runs once at server start.
func (s *Server) flushAllMail() {
	if dispatch.DBExists(s.session) {
		if store, err := s.db(); err == nil {
			if msgs, err := store.PendingJobMessages(); err == nil {
				for _, m := range msgs {
					if _, e := s.resolveTarget(m.ToAgent, false); e != nil {
						_ = store.FailMessage(m.ID, e.Message)
					}
				}
			}
		}
	}

	s.eachPane(func(pid, _, _ int) {
		if regularMailCanSubmit(s.app.PaneStatusKey(pid)) {
			s.flushMailForPane(pid)
		}
	})
}

// encodeMailBody renders a message body plus a submit Enter as PTY bytes,
// wrapped in bracketed-paste markers when the pane negotiated that mode.
func encodeMailBody(body string, bracketed bool) []byte {
	var b bytes.Buffer
	if bracketed {
		b.WriteString(string(ansi.BracketedPasteStart))
	}
	b.WriteString(body)
	if bracketed {
		b.WriteString(string(ansi.BracketedPasteEnd))
	}
	b.WriteByte('\r')
	return b.Bytes()
}

// encodeMailFor encodes a body for the pane's negotiated input mode.
func (s *Server) encodeMailFor(pid int, body string) []byte {
	if p := s.sup.Get(pid); p != nil {
		return encodeMailBody(body, p.BracketedPasteMode())
	}
	return encodeMailBody(body, false)
}

func (s *Server) failJobQueue(n dispatch.PendingNudge, reason string) {
	if store, err := s.db(); err == nil {
		if msgs, err := store.PendingMessagesForAgent(n.Room, n.ToAgent); err == nil {
			for _, m := range msgs {
				_ = store.FailMessage(m.ID, reason)
			}
		}
	}
}

// A pane closed after enqueue is a failed destination, while a surviving
// owner with no ready runtime must retain its durable queue.
func (s *Server) failMissingJobWrite(w mailWrite) {
	if w.room != dispatch.JobsRoom {
		return
	}
	s.app.Do(func() { s.failMissingJobWriteOnLoop(w) })
}
func (s *Server) failMissingJobWriteOnLoop(w mailWrite) {
	if _, err := s.resolveTarget(fmtPaneID(w.pane), false); err != nil {
		if store, e := s.db(); e == nil {
			for _, id := range w.msgIDs {
				_ = store.FailMessage(id, err.Message)
			}
		}
	}
}
