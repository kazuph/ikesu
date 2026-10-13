package server

// Domain event bus (SPEC G1 §events): every command is diffed against a
// pre-state snapshot and the differences become journal entries and live
// pushes. events.subscribe replays the journal then streams live;
// events.wait resolves on a matching future event.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

const (
	eventJournalCap  = 2000
	eventSubBuffer   = 1024
	eventWaitPoll    = 50 * time.Millisecond
	eventWaitDefault = 30 * time.Second
)

// subscriptionKinds maps the dotted subscription names to emit kinds
// (schema Subscription oneOf). Unknown names are rejected.
var subscriptionKinds = func() map[string]bool {
	names := []string{
		"decision.created", "decision.resolved",
		"workspace.created", "workspace.updated", "workspace.metadata_updated",
		"workspace.renamed", "workspace.moved", "workspace.closed",
		"workspace.focused",
		"worktree.created", "worktree.opened", "worktree.removed",
		"tab.created", "tab.closed", "tab.focused", "tab.renamed", "tab.moved",
		"pane.created", "pane.closed", "pane.updated", "pane.focused",
		"pane.moved", "pane.exited", "pane.agent_detected",
		"pane.output_matched", "pane.agent_status_changed",
		"pane.scroll_changed", "layout.updated",
	}
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}()

// dottedToKind converts "pane.focused" → "pane_focused".
func dottedToKind(dotted string) string { return strings.ReplaceAll(dotted, ".", "_") }

type eventSub struct {
	types map[string]bool
	ch    chan api.EventEnvelope
}

type eventWaiter struct {
	match func(env api.EventEnvelope) bool
	ch    chan api.EventEnvelope
}

// emitEvent records and delivers an event. Safe on the app loop: pushes
// are non-blocking; slow subscribers drop events.
func (s *Server) emitEvent(kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	s.pluginEvent(kind, raw)
	env := api.EventEnvelope{Event: kind, Data: raw}
	s.evMu.Lock()
	s.journal = append(s.journal, env)
	if len(s.journal) > eventJournalCap {
		s.journal = append([]api.EventEnvelope(nil), s.journal[len(s.journal)-eventJournalCap:]...)
	}
	var hit []*eventWaiter
	var keep []*eventWaiter
	for _, w := range s.waiters {
		if w.match(env) {
			hit = append(hit, w)
		} else {
			keep = append(keep, w)
		}
	}
	s.waiters = keep
	for sub := range s.subs {
		if sub.types[kind] {
			select {
			case sub.ch <- env:
			default:
			}
		}
	}
	s.evMu.Unlock()
	for _, w := range hit {
		select {
		case w.ch <- env:
		default:
		}
	}
}

// ev is a convenience for building typed event data maps.
func ev(kind string, kv ...any) map[string]any {
	m := map[string]any{"type": kind}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// --- command-diff emission ---

// afterExec derives domain events from the pre/post command diff. It runs
// on the app loop after effects are applied.
func (s *Server) afterExec(cmd model.Command, prev *model.StateSnap) {
	follow := prev.Active != s.app.State().Active || prev.FocusedPane != s.app.State().FocusedPane()
	switch c := cmd.(type) {
	case model.CmdSelectWorkspace, model.CmdSelectTab, model.CmdFocusPane:
		// An explicit focus also applies when the canonical selection was
		// already there but an attached viewer navigated elsewhere.
		follow = true
	case model.CmdMovePane:
		follow = c.Focus && c.Out != nil && c.Out.Changed
	}
	if !s.app.InViewer() && follow {
		for _, c := range s.displayClients() {
			if c.viewer != nil {
				c.viewer.FollowSessionFocus(s.app)
			}
		}
		s.mu.Lock()
		foreground := s.client
		s.mu.Unlock()
		if foreground != nil && foreground.viewer != nil {
			foreground.viewer.AcquireSize(s.app)
		}
	}
	st := s.app.State()

	prevWs := map[model.WorkspaceID]model.WsSnap{}
	for _, w := range prev.Workspaces {
		prevWs[w.ID] = w
	}
	curWs := map[model.WorkspaceID]int{}
	for i, ws := range st.Workspaces {
		curWs[ws.ID] = i
	}

	// Closed workspaces first (their tabs and panes go with them).
	for _, pw := range prev.Workspaces {
		if _, ok := curWs[pw.ID]; ok {
			continue
		}
		delete(s.workspaceMetadata, int(pw.ID))
		for _, pt := range pw.Tabs {
			s.emitEvent("tab_closed", ev("tab_closed",
				"tab_id", fmtTabID(int(pw.ID), pt.ID),
				"workspace_id", fmtWorkspaceID(int(pw.ID))))
		}
		s.emitEvent("workspace_closed", ev("workspace_closed",
			"workspace_id", fmtWorkspaceID(int(pw.ID))))
	}
	// Created workspaces.
	for i, ws := range st.Workspaces {
		if _, ok := prevWs[ws.ID]; !ok {
			s.emitEvent("workspace_created", ev("workspace_created", "workspace", s.wsInfo(i)))
		}
	}
	// Surviving workspaces: renamed/moved/updated/focused + tab diffs.
	prevOrder := make([]int, 0, len(prev.Workspaces))
	for _, pw := range prev.Workspaces {
		if _, ok := curWs[pw.ID]; ok {
			prevOrder = append(prevOrder, int(pw.ID))
		}
	}
	curOrder := make([]int, 0, len(st.Workspaces))
	for _, ws := range st.Workspaces {
		if _, ok := prevWs[ws.ID]; ok {
			curOrder = append(curOrder, int(ws.ID))
		}
	}
	moved := !sameInts(prevOrder, curOrder)
	for i, ws := range st.Workspaces {
		pw, ok := prevWs[ws.ID]
		if !ok {
			continue
		}
		if pw.Worktree != ws.Worktree || pw.GroupCollapsed != ws.GroupCollapsed {
			s.emitEvent("workspace_metadata_updated", ev("workspace_metadata_updated", "workspace", s.wsInfo(i)))
		}
		if pw.Name != ws.Name {
			s.emitEvent("workspace_renamed", ev("workspace_renamed",
				"workspace_id", fmtWorkspaceID(int(ws.ID)), "label", ws.Name))
		}
		if moved {
			s.emitEvent("workspace_moved", ev("workspace_moved",
				"workspace_id", fmtWorkspaceID(int(ws.ID)),
				"insert_index", i, "workspaces", wsIDList(st)))
			break // one workspace_moved per reorder
		}
		if pw.Panes != ws.PaneCount() || len(pw.Tabs) != len(ws.Tabs) {
			s.emitEvent("workspace_updated", ev("workspace_updated", "workspace", s.wsInfo(i)))
		}
		s.diffTabs(int(ws.ID), i, pw, ws)
	}
	if prev.Active != st.Active && st.Active >= 0 && st.Active < len(st.Workspaces) {
		s.emitEvent("workspace_focused", ev("workspace_focused",
			"workspace_id", fmtWorkspaceID(int(st.Workspaces[st.Active].ID))))
	}

	// Pane-level diffs.
	curPanes := map[int]model.PaneRef{}
	for wi, ws := range st.Workspaces {
		for tj, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				curPanes[pid] = model.PaneRef{WSIdx: wi, TabIdx: tj, WSID: ws.ID, TabID: t.ID}
			}
		}
	}
	for pid, pr := range prev.Panes {
		if _, ok := curPanes[pid]; !ok {
			s.emitEvent("pane_closed", ev("pane_closed",
				"pane_id", fmtPaneID(pid), "workspace_id", fmtWorkspaceID(int(pr.WSID))))
		}
	}
	for pid, pr := range curPanes {
		if _, ok := prev.Panes[pid]; !ok {
			_ = pr
			if info, ok := s.paneInfo(pid); ok {
				s.emitEvent("pane_created", ev("pane_created", "pane", info))
			}
		}
	}
	// Per-tab focus and layout changes.
	for i, ws := range st.Workspaces {
		pw, ok := prevWs[ws.ID]
		if !ok {
			continue
		}
		prevTabs := map[int]model.TabSnap{}
		for _, pt := range pw.Tabs {
			prevTabs[pt.ID] = pt
		}
		for j, t := range ws.Tabs {
			pt, ok := prevTabs[t.ID]
			if !ok {
				continue
			}
			if pt.Focused != t.Layout.Focused() {
				s.emitEvent("pane_focused", ev("pane_focused",
					"pane_id", fmtPaneID(t.Layout.Focused()),
					"workspace_id", fmtWorkspaceID(int(ws.ID))))
			}
			if pt.FP != t.Layout.Fingerprint() {
				s.emitEvent("layout_updated", ev("layout_updated",
					"layout", s.layoutSnapshot(i, j)))
			}
		}
	}

	// Command-specific events.
	switch c := cmd.(type) {
	case model.CmdMovePane:
		if c.Out != nil && c.Out.Changed {
			s.emitPaneMoved(c.Out)
		}
	case model.CmdRenamePane:
		if info, ok := s.paneInfo(c.Pane); ok {
			s.emitEvent("pane_updated", ev("pane_updated", "pane", info))
		}
	case model.CmdPaneExited:
		// pane.exited itself is emitted by the exited hook before this
		// command; the close above covers pane.closed.
	}
}

func (s *Server) emitPaneMoved(out *model.PaneMoveOutcome) {
	info, _ := s.paneInfo(out.Pane)
	d := ev("pane_moved",
		"previous_pane_id", fmtPaneID(out.Pane),
		"previous_workspace_id", fmtWorkspaceID(out.PrevWsID),
		"previous_tab_id", fmtTabID(out.PrevWsID, out.PrevTabID),
		"pane", info)
	if out.NewTabID != 0 {
		d["created_tab"] = s.tabInfo(out.TgtWsIdx, out.TgtTabIdx)
	}
	if out.NewWsID != 0 {
		d["created_workspace"] = s.wsInfo(out.NewWsIdx)
	}
	if out.ClosedTabID != 0 {
		d["closed_tab_id"] = fmtTabID(out.PrevWsID, out.ClosedTabID)
	}
	if out.ClosedWsID != 0 {
		d["closed_workspace_id"] = fmtWorkspaceID(out.ClosedWsID)
	}
	s.emitEvent("pane_moved", d)
}

// diffTabs emits tab events for one workspace.
func (s *Server) diffTabs(wsID, wsIdx int, pw model.WsSnap, ws *model.Workspace) {
	curTabs := map[int]int{}
	for j, t := range ws.Tabs {
		curTabs[t.ID] = j
	}
	for _, pt := range pw.Tabs {
		if _, ok := curTabs[pt.ID]; !ok {
			s.emitEvent("tab_closed", ev("tab_closed",
				"tab_id", fmtTabID(wsID, pt.ID), "workspace_id", fmtWorkspaceID(wsID)))
		}
	}
	prevOrder := make([]int, 0, len(pw.Tabs))
	for _, pt := range pw.Tabs {
		if _, ok := curTabs[pt.ID]; ok {
			prevOrder = append(prevOrder, pt.ID)
		}
	}
	curOrder := make([]int, 0, len(ws.Tabs))
	ptSet := map[int]bool{}
	for _, pt := range pw.Tabs {
		ptSet[pt.ID] = true
	}
	for _, t := range ws.Tabs {
		if ptSet[t.ID] {
			curOrder = append(curOrder, t.ID)
		}
	}
	moved := !sameInts(prevOrder, curOrder)
	ptMap := map[int]model.TabSnap{}
	for _, pt := range pw.Tabs {
		ptMap[pt.ID] = pt
	}
	for j, t := range ws.Tabs {
		if pt, ok := ptMap[t.ID]; !ok {
			s.emitEvent("tab_created", ev("tab_created", "tab", s.tabInfo(wsIdx, j)))
		} else {
			if pt.Name != t.Name {
				s.emitEvent("tab_renamed", ev("tab_renamed",
					"tab_id", fmtTabID(wsID, t.ID),
					"workspace_id", fmtWorkspaceID(wsID),
					"label", t.Name))
			}
			if moved {
				s.emitEvent("tab_moved", ev("tab_moved",
					"tab_id", fmtTabID(wsID, t.ID),
					"workspace_id", fmtWorkspaceID(wsID),
					"insert_index", j,
					"tabs", tabIDList(wsID, ws)))
				moved = false
			}
		}
	}
	if pw.ActiveTab != ws.ActiveTab && ws.ActiveTab >= 0 && ws.ActiveTab < len(ws.Tabs) {
		s.emitEvent("tab_focused", ev("tab_focused",
			"tab_id", fmtTabID(wsID, ws.Tabs[ws.ActiveTab].ID),
			"workspace_id", fmtWorkspaceID(wsID)))
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func wsIDList(st *model.AppState) []string {
	out := make([]string, 0, len(st.Workspaces))
	for _, ws := range st.Workspaces {
		out = append(out, fmtWorkspaceID(int(ws.ID)))
	}
	return out
}

func tabIDList(wsID int, ws *model.Workspace) []string {
	out := make([]string, 0, len(ws.Tabs))
	for _, t := range ws.Tabs {
		out = append(out, fmtTabID(wsID, t.ID))
	}
	return out
}

// --- runtime hooks ---

// onPaneExited emits pane.exited for a dead process.
func (s *Server) onPaneExited(pid int) {
	wi, _ := s.app.State().FindPane(pid)
	if wi < 0 {
		return
	}
	s.emitEvent("pane_exited", ev("pane_exited",
		"pane_id", fmtPaneID(pid),
		"workspace_id", fmtWorkspaceID(int(s.app.State().Workspaces[wi].ID))))
}

// onPaneOutput emits pane.output_changed, throttled per pane.
func (s *Server) onPaneOutput(pid int) {
	s.scheduleScreenPrompts()
	p := s.app.Supervisor().Get(pid)
	if p == nil {
		return
	}
	sn := p.Snapshot()
	s.evMu.Lock()
	lr, ok := s.outEmit[pid]
	if !ok {
		lr = &outputEmit{}
		s.outEmit[pid] = lr
	}
	if sn.Revision == lr.rev || time.Since(lr.at) < 100*time.Millisecond {
		s.evMu.Unlock()
		return
	}
	lr.rev = sn.Revision
	lr.at = time.Now()
	s.evMu.Unlock()
	wi, _ := s.app.State().FindPane(pid)
	if wi < 0 {
		return
	}
	s.emitEvent("pane_output_changed", ev("pane_output_changed",
		"pane_id", fmtPaneID(pid),
		"workspace_id", fmtWorkspaceID(int(s.app.State().Workspaces[wi].ID)),
		"revision", sn.Revision))
}

// onAgentStatus emits pane.agent_status_changed / pane.agent_detected.
// Called from onPaneStatus (avail hook).
func (s *Server) emitAgentEvents(pid int) {
	wi, _ := s.app.State().FindPane(pid)
	if wi < 0 {
		return
	}
	wsID := fmtWorkspaceID(int(s.app.State().Workspaces[wi].ID))
	status := s.app.PaneStatusKey(pid)
	agent := ""
	if a, _, ok := s.app.Reported(pid); ok {
		agent = a
	}
	if agent == "" {
		if p := s.app.Supervisor().Get(pid); p != nil {
			agent = p.Snapshot().AgentID
		}
	}
	if prev := s.lastAgent[pid]; prev != agent {
		s.lastAgent[pid] = agent
		if prev == "" && agent != "" {
			s.emitEvent("pane_agent_detected", ev("pane_agent_detected",
				"pane_id", fmtPaneID(pid), "workspace_id", wsID,
				"agent", agent, "final_status", status, "released", false))
		}
	}
	s.emitEvent("pane_agent_status_changed", ev("pane_agent_status_changed",
		"pane_id", fmtPaneID(pid), "workspace_id", wsID,
		"agent_status", status, "agent", agent))
}

// --- subscribe / wait ---

// serveEvents handles events.subscribe: it writes subscription_started,
// replays the matching journal, then streams live events until the
// connection dies. It takes over the connection.
func (s *Server) serveEvents(conn net.Conn, req *api.Request) {
	var p api.EventsSubscribeParams
	if err := req.DecodeParams(&p); err != nil {
		_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest, err.Error()))
		return
	}
	types := map[string]bool{}
	for _, sub := range p.Subscriptions {
		if !subscriptionKinds[sub.Type] {
			expected := make([]string, 0, len(subscriptionKinds))
			for k := range subscriptionKinds {
				expected = append(expected, k)
			}
			_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest,
				fmt.Sprintf("unknown variant `%s`, expected one of `%s`",
					sub.Type, strings.Join(sortedKeys(expected), "`, `"))))
			return
		}
		types[dottedToKind(sub.Type)] = true
	}
	// Snapshot the journal, then register the live channel so events
	// emitted during the replay are not duplicated.
	s.evMu.Lock()
	journal := append([]api.EventEnvelope(nil), s.journal...)
	sub := &eventSub{types: types, ch: make(chan api.EventEnvelope, eventSubBuffer)}
	if s.subs == nil {
		s.subs = map[*eventSub]bool{}
	}
	s.subs[sub] = true
	s.evMu.Unlock()
	defer func() {
		s.evMu.Lock()
		delete(s.subs, sub)
		s.evMu.Unlock()
	}()

	if err := api.WriteResponse(conn, api.Success(req.ID,
		api.SubscriptionStartedResult{Type: "subscription_started"})); err != nil {
		return
	}
	for _, env := range journal {
		if !types[env.Event] {
			continue
		}
		if !writeEvent(conn, env) {
			return
		}
	}
	// Detect client disconnect while no events are flowing.
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		case <-s.sup.Done():
			return
		case env := <-sub.ch:
			if !writeEvent(conn, env) {
				return
			}
		}
	}
}

func writeEvent(conn net.Conn, env api.EventEnvelope) bool {
	b, err := json.Marshal(env)
	if err != nil {
		return true
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err == nil
}

// matchFilters are the match_event fields compared against same-named
// fields in the event data payload.
var matchFilters = map[string]bool{
	"workspace_id": true, "tab_id": true, "pane_id": true,
	"label": true, "agent": true, "agent_status": true,
	"state": true, "agent_source": true,
}

// eventsWait handles events.wait outside the app loop.
func (s *Server) eventsWait(req *api.Request) api.Response {
	var p api.EventsWaitParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	event, _ := p.MatchEvent["event"].(string)
	// The released binary only supports pane agent-status matches; other
	// schema variants are rejected with unsupported_event_wait_match.
	if event != "pane_agent_status_changed" {
		return api.Failure(req.ID, api.ErrUnsupportedWait,
			"events.wait currently supports pane agent status matches")
	}
	filters := map[string]string{}
	for k, v := range p.MatchEvent {
		if k == "event" || !matchFilters[k] {
			continue
		}
		if sv, ok := v.(string); ok && sv != "" {
			filters[k] = sv
		}
	}
	if paneID, ok := filters["pane_id"]; ok {
		var found bool
		s.app.Do(func() {
			_, wi, _, ok := s.findPane(paneID)
			found = ok && wi >= 0
		})
		if !found {
			return s.paneNotFound(req.ID, paneID)
		}
	}
	match := func(env api.EventEnvelope) bool {
		if env.Event != event {
			return false
		}
		if len(filters) == 0 {
			return true
		}
		var d map[string]any
		if json.Unmarshal(env.Data, &d) != nil {
			return false
		}
		for k, want := range filters {
			got, _ := d[k].(string)
			if got != want {
				return false
			}
		}
		return true
	}
	timeout := eventWaitDefault
	if p.TimeoutMs != nil {
		timeout = time.Duration(*p.TimeoutMs) * time.Millisecond
	}
	w := &eventWaiter{match: match, ch: make(chan api.EventEnvelope, 1)}
	s.evMu.Lock()
	s.waiters = append(s.waiters, w)
	s.evMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	defer func() {
		s.evMu.Lock()
		for i, x := range s.waiters {
			if x == w {
				s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
				break
			}
		}
		s.evMu.Unlock()
	}()
	select {
	case env := <-w.ch:
		return api.Success(req.ID, api.WaitMatchedResult{Type: "wait_matched", Event: &env})
	case <-timer.C:
		return api.Failure(req.ID, api.ErrTimeout,
			fmt.Sprintf("timed out after %s waiting for event", timeout))
	}
}

type outputEmit struct {
	rev uint64
	at  time.Time
}

func sortedKeys(m []string) []string {
	for i := 0; i < len(m); i++ {
		for j := i + 1; j < len(m); j++ {
			if m[j] < m[i] {
				m[i], m[j] = m[j], m[i]
			}
		}
	}
	return m
}

// waitForOutput handles pane.wait_for_output off the app loop: it polls the
// pane's text until the match hits or the deadline passes.
func (s *Server) waitForOutput(req *api.Request) api.Response {
	var p api.PaneWaitForOutputParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	var matchRE *regexp.Regexp
	switch p.Match.Type {
	case "substring":
	case "regex":
		re, err := regexp.Compile(p.Match.Value)
		if err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest,
				fmt.Sprintf("invalid regex: %v", err))
		}
		matchRE = re
	default:
		return api.Failure(req.ID, api.ErrInvalidRequest,
			fmt.Sprintf("unknown match type %q", p.Match.Type))
	}
	source := p.Source
	if source == "" {
		source = "recent"
	}
	if source != "visible" && source != "recent" && source != "recent_unwrapped" && source != "recent-unwrapped" {
		return api.Failure(req.ID, api.ErrInvalidRequest,
			fmt.Sprintf("read source %q is not supported in this stage", source))
	}
	limit := 80
	if p.Lines != nil {
		limit = int(*p.Lines)
		if limit > 1000 {
			limit = 1000
		}
	}
	timeout := eventWaitDefault
	if p.TimeoutMs != nil {
		timeout = time.Duration(*p.TimeoutMs) * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	matches := func(line string) bool {
		if matchRE != nil {
			return matchRE.MatchString(line)
		}
		return strings.Contains(line, p.Match.Value)
	}
	for {
		var resp api.Response
		var again bool
		s.app.Do(func() {
			pid, wi, ti, ok := s.findPane(p.PaneID)
			if !ok {
				resp = s.paneNotFound(req.ID, p.PaneID)
				return
			}
			pane := s.app.Supervisor().Get(pid)
			if pane == nil {
				resp = s.paneNotFound(req.ID, p.PaneID)
				return
			}
			strip := p.StripAnsi == nil || *p.StripAnsi
			lines, err := pane.ReadLines(source, strip)
			if err != nil {
				resp = api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
				return
			}
			for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
				lines = lines[:len(lines)-1]
			}
			truncated := false
			if len(lines) > limit {
				lines = lines[len(lines)-limit:]
				truncated = true
			}
			matchedLine := ""
			for _, line := range lines {
				if matches(line) {
					matchedLine = line
					break
				}
			}
			if matchedLine == "" {
				again = true
				return
			}
			sn := pane.Snapshot()
			ws := s.app.State().Workspaces[wi]
			read := api.PaneReadResult{
				PaneID:      fmtPaneID(pid),
				WorkspaceID: fmtWorkspaceID(ws.ID),
				TabID:       fmtTabID(ws.ID, ws.Tabs[ti].ID),
				Source:      source,
				Format:      "text",
				Text:        strings.Join(lines, "\n"),
				Revision:    sn.Revision,
				Truncated:   truncated,
			}
			raw, _ := json.Marshal(read)
			resp = api.Success(req.ID, api.OutputMatchedResult{
				Type:        "output_matched",
				PaneID:      fmtPaneID(pid),
				Revision:    sn.Revision,
				MatchedLine: matchedLine,
				Read:        raw,
			})
		})
		if resp.Result != nil || resp.Err != nil {
			return resp
		}
		if !again || time.Now().After(deadline) {
			return api.Failure(req.ID, api.ErrTimeout,
				fmt.Sprintf("timed out after %s waiting for output", timeout))
		}
		time.Sleep(eventWaitPoll)
	}
}
