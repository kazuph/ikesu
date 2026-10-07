package server

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
)

// Durable mailbox: msg.* API handlers, recipient resolution, and the
// queued→in_flight→delivered delivery pipeline. Semantics mirror herdr's
// src/app/msg.rs: regular mail is addressed to a pane id and submitted only
// when the pane's status is idle/working/done; job mail (room gtx-jobs) is
// addressed to the agent's mailbox name and delivered to anything that is
// not blocked. A message is marked delivered only after every byte reached
// the pane's PTY.

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
	for w := range s.mailCh {
		p := s.sup.Get(w.pane)
		if p == nil {
			s.mailDone(w)
			continue
		}
		if _, err := p.WriteToPTY(w.payload); err != nil {
			s.mailDone(w)
			continue
		}
		if store, err := s.db(); err == nil {
			if w.msgID != 0 {
				_, _ = store.MarkMessageDelivered(w.msgID)
			} else if len(w.msgIDs) > 0 {
				_, _ = store.MarkDeliveredByIDs(w.msgIDs)
			}
		}
		s.mailDone(w)
	}
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
		s.store, s.dbErr = dispatch.Open(dispatch.DBPath())
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
	recipient, err := s.resolveMsgRecipient(room, p.To)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	store, derr := s.db()
	if derr != nil {
		return api.Failure(id, api.ErrMsgStore, derr.Error())
	}
	m, derr := store.InsertMessage(room, p.Project, from, recipient, p.Body, p.ReplyTo, false)
	if derr != nil {
		return api.Failure(id, api.ErrMsgStore, derr.Error())
	}
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
	if room == dispatch.JobsRoom {
		name, ok := s.mailboxName(t.pid)
		if !ok {
			return "", &api.Error{Code: api.ErrAgentNotFound,
				Message: fmt.Sprintf("agent target %s has no reported agent identity", target)}
		}
		return name, nil
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
	if room == dispatch.JobsRoom {
		if name, ok := s.mailboxName(t.pid); ok && name != aliases[0] {
			if other, oerr := s.resolveTarget(name, false); oerr == nil && other.pid == t.pid {
				aliases = append(aliases, name)
			}
		}
	}
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
	if !dispatch.DBExists() {
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
	if !dispatch.DBExists() {
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
	t, err := s.resolveTarget(n.ToAgent, false)
	if err != nil {
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
	if name, ok := s.mailboxName(pid); ok {
		s.flushJobNudgesFor(name)
	}
}

// flushAllMail runs once at server start.
func (s *Server) flushAllMail() {
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
