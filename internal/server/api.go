package server

import (
	"bufio"
	"net"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
)

// apiLoop accepts API connections until the listener closes.
func (s *Server) apiLoop() {
	for {
		conn, err := s.apiLn.Accept()
		if err != nil {
			return
		}
		s.apiWG.Add(1)
		go s.serveAPI(conn)
	}
}

// serveAPI reads newline-delimited requests and writes one response per
// request until the connection closes.
func (s *Server) serveAPI(conn net.Conn) {
	defer s.apiWG.Done()
	defer conn.Close()
	rd := bufio.NewReader(conn)
	for {
		req, err := api.ReadRequest(rd)
		if err != nil {
			return
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
		case api.MethodPaneGraphicsStream:
			s.serveGraphicsStream(conn, rd, req)
			return
		case api.MethodDecisionWait:
			s.serveDecisionWait(conn, req)
			return
		case api.MethodEventsSubscribe:
			s.serveEvents(conn, req)
			return
		case api.MethodEventsWait:
			if err := api.WriteResponse(conn, s.eventsWait(req)); err != nil {
				return
			}
			continue
		case api.MethodAgentPrompt:
			if err := api.WriteResponse(conn, s.agentPrompt(req)); err != nil {
				return
			}
			continue
		case api.MethodPaneWaitForOutput:
			if err := api.WriteResponse(conn, s.waitForOutput(req)); err != nil {
				return
			}
			continue
		}
		resp := s.dispatch(req)
		if err := api.WriteResponse(conn, resp); err != nil {
			return
		}
	}
}

// dispatch routes a request. Stateless methods answer directly; everything
// touching application state is serialized through the app loop via Do.
func (s *Server) dispatch(req *api.Request) (response api.Response) {
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
	if response, blocked := s.rejectOverlayLayoutRequest(req); blocked {
		return response
	}
	if req.Method == "agent.view.set" || req.Method == "agent.view.clear" {
		var response api.Response
		s.app.Do(func() { response = s.agentViewRequest(req) })
		return response
	}
	if strings.HasPrefix(req.Method, "decision.") {
		var resp api.Response
		var delivery <-chan error
		s.app.Do(func() {
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
			}
		}
		return resp
	}
	if strings.HasPrefix(req.Method, "plugin.") {
		var resp api.Response
		s.app.Do(func() { resp = s.pluginRequest(req) })
		return resp
	}
	switch req.Method {
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
		s.app.Do(func() { resp = s.agentStart(req.ID, p) })
		return resp
	case api.MethodServerLiveHandoff:
		return s.liveHandoff(req)
	case api.MethodPing:
		return api.Success(req.ID, api.PongResult{
			Type:         "pong",
			Version:      s.version,
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
		s.app.Do(func() {
			resp = s.dispatchOnLoop(req)
		})
		return resp
	}
}
