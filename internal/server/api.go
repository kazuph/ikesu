package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/latency"
)

// apiLoop accepts API connections until the listener closes.
func (s *Server) apiLoop() {
	for {
		conn, err := acceptWithRetry(s.apiLn, func() bool { return s.closing.Load() != 0 })
		if err != nil {
			return
		}
		s.apiWG.Add(1)
		peer := latency.PeerIdentityOf(conn)
		_, start, err := guard.ApprovalProcess(peer.PID)
		if err != nil {
			peer = latency.PeerIdentity{}
		}
		go s.serveAPIWithPeer(conn, peer, start)
	}
}

// serveAPI reads newline-delimited requests and writes one response per
// request until the connection closes.
func (s *Server) serveAPI(conn net.Conn) {
	peer := latency.PeerIdentityOf(conn)
	_, start, err := guard.ApprovalProcess(peer.PID)
	if err != nil {
		peer = latency.PeerIdentity{}
	}
	s.serveAPIWithPeer(conn, peer, start)
}

func (s *Server) serveAPIWithPeer(conn net.Conn, accepted latency.PeerIdentity, acceptedStart string) {
	defer s.apiWG.Done()
	defer conn.Close()
	ctx, stopPeer, err := peerContext(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ikesu: monitor API connection: %v\n", err)
		return
	}
	defer stopPeer()
	rd := bufio.NewReader(conn)
	for {
		req, err := api.ReadRequest(rd)
		if err != nil {
			return
		}
		current := latency.PeerIdentityOf(conn)
		_, start, peerErr := guard.ApprovalProcess(accepted.PID)
		if accepted.PID > 1 && accepted == current && peerErr == nil && start == acceptedStart {
			req.PeerPID = accepted.PID
		}
		if s.closing.Load() != 0 {
			// Pending waits reconnect to the successor with the same decision ID.
			if req.Method == api.MethodDecisionWait {
				return
			}
			_ = api.WriteResponse(conn, api.Failure(req.ID, "server_stopping", "server is stopping"))
			return
		}
		// events.subscribe turns this connection into an event stream;
		// events.wait may block for the timeout, off the app loop.
		switch req.Method {
		case api.MethodServerProfile, api.MethodServerStacks:
			if !sameUserPeer(conn) {
				_ = api.WriteResponse(conn, api.Failure(req.ID, "permission_denied", "diagnostics require a same-user Unix socket peer"))
				continue
			}
			if err := api.WriteResponse(conn, s.diagnostics(req)); err != nil {
				return
			}
			continue
		case api.MethodPaneGraphicsStream:
			s.serveGraphicsStream(ctx, conn, rd, req)
			return
		case api.MethodDecisionWait:
			s.serveDecisionWait(ctx, conn, req)
			return
		case api.MethodEventsSubscribe:
			s.serveEvents(conn, req)
			return
		case api.MethodEventsWait:
			if err := api.WriteResponse(conn, s.eventsWait(ctx, req)); err != nil {
				return
			}
			continue
		case api.MethodAgentPrompt:
			if err := api.WriteResponse(conn, s.agentPrompt(ctx, req)); err != nil {
				return
			}
			continue
		case api.MethodPaneWaitForOutput:
			if err := api.WriteResponse(conn, s.waitForOutput(ctx, req)); err != nil {
				return
			}
			continue
		}
		resp := s.dispatchContext(ctx, req)
		if err := api.WriteResponse(conn, resp); err != nil {
			return
		}
	}
}

// dispatch routes a request. Stateless methods answer directly; everything
// touching application state is serialized through the app loop via Do.
func (s *Server) dispatch(req *api.Request) (response api.Response) {
	return s.dispatchContext(context.Background(), req)
}

func (s *Server) dispatchContext(ctx context.Context, req *api.Request) (response api.Response) {
	stopping := api.Failure(req.ID, "server_stopping", "server is stopping")
	response = stopping
	// Do may return without running a callback once the app exits.
	defer func() {
		if len(response.Result) == 0 && response.Err == nil {
			response = stopping
		}
	}()
	if s.closing.Load() != 0 {
		return stopping
	}
	if req.Method == api.MethodGuardIdentity {
		var resp api.Response
		s.app.DoContext(ctx, func() { resp = s.guardIdentity(req) })
		return resp
	}
	if response, blocked := s.rejectOverlayLayoutRequestContext(ctx, req); blocked {
		return response
	}
	if req.Method == api.MethodMsgSend {
		var p api.MsgSendParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		var result api.Response
		var query mailSendQuery
		s.app.DoContext(ctx, func() { result = s.prepareMsgSend(req.ID, p, &query) })
		if !query.ready {
			return result
		}
		defer func() {
			s.mailMu.Lock()
			delete(s.mailSendPending, query.message.ID)
			s.mailMu.Unlock()
		}()
		var delivered bool
		for {
			if !s.beginMailQuery() {
				return stopping
			}
			snapshot := func() paneMailSnapshot {
				defer s.mailQueryWG.Done()
				return s.readPaneMail(query.pid)
			}()
			if query.room == dispatch.JobsRoom {
				snapshot.regular = nil
			} else {
				snapshot.jobs = nil
			}
			var stale bool
			s.app.DoContext(ctx, func() { stale = s.applyPaneMailSnapshot(query.pid, query.target, snapshot, &delivered) })
			if !stale {
				break
			}
		}
		var nudged []string
		s.mailMu.Lock()
		delivered = delivered || query.nudged
		s.mailMu.Unlock()
		if delivered {
			nudged = []string{query.recipient}
		}
		return api.Success(req.ID, api.MsgSendResult{Type: "msg_send", Messages: []api.MsgMessage{query.message}, Nudged: nudged})
	}
	if req.Method == "agent.view.set" || req.Method == "agent.view.clear" {
		var response api.Response
		s.app.DoContext(ctx, func() { response = s.agentViewRequest(req) })
		return response
	}
	if strings.HasPrefix(req.Method, "decision.") {
		var resp api.Response
		var delivery <-chan error
		s.app.DoContext(ctx, func() {
			resp = s.decisionRequest(req)
			if req.Method == api.MethodDecisionAnswer && resp.Err == nil {
				var p api.DecisionAnswerParams
				if req.DecodeParams(&p) == nil {
					for _, w := range s.screenPrompts.watches {
						if w.decisionID == p.DecisionID {
							delivery = w.delivery
						}
					}
				}
			}
		})
		if delivery != nil {
			select {
			case err := <-delivery:
				if err != nil {
					return api.Failure(req.ID, "screen_prompt_input_failed", err.Error())
				}
			case <-s.decisions.stopped:
				return api.Failure(req.ID, "screen_prompt_input_failed", "server stopped during input delivery")
			case <-ctx.Done():
				return api.Failure(req.ID, "screen_prompt_input_failed", "request canceled while waiting for delivery")
			}
		}
		return resp
	}
	if strings.HasPrefix(req.Method, "plugin.") {
		var resp api.Response
		s.app.DoContext(ctx, func() { resp = s.pluginRequest(req) })
		return resp
	}
	switch req.Method {
	case api.MethodClientViewList:
		return api.Success(req.ID, s.clientViewList())
	case api.MethodAgentStart:
		var p api.AgentStartParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		if len(p.Argv) > 0 {
			if err := s.sup.WaitLoginPath(); err != nil {
				return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
			}
		}
		var resp api.Response
		s.app.DoContext(ctx, func() { resp = s.agentStart(req.ID, p) })
		return resp
	case api.MethodServerLiveHandoff:
		return s.liveHandoff(ctx, req)
	case api.MethodPing:
		var stall *api.LoopStallStatus
		if s.app != nil {
			value := s.stallStatus()
			stall = &value
		}
		return api.Success(req.ID, api.PongResult{
			Type:         "pong",
			LoopStall:    stall,
			Version:      s.version,
			Executable:   s.executable,
			Generation:   s.generation,
			Protocol:     api.Protocol,
			Capabilities: map[string]any{"live_handoff": true, "screen_prompt_decisions": true},
		})
	case api.MethodServerStop:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		// Respond ok, then quit on the app loop. Stop posts the command;
		// the response is written while teardown proceeds.
		go s.app.Stop()
		return api.Success(req.ID, api.OkResult{Type: "ok"})
	case api.MethodServerRestart:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		// Same shape as server.stop: respond ok, then save+quit on the
		// app loop; cleanup respawns a fresh server for the client to
		// reconnect to.
		go s.app.Do(s.requestRestart)
		return api.Success(req.ID, api.OkResult{Type: "ok"})
	default:
		var resp api.Response
		s.app.DoContext(ctx, func() {
			resp = s.dispatchOnLoop(req)
		})
		return resp
	}
}
