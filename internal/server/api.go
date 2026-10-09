package server

import (
	"bufio"
	"net"

	"github.com/kazuph/goterrace/internal/api"
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
		// events.subscribe turns this connection into an event stream;
		// events.wait may block for the timeout, off the app loop.
		switch req.Method {
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
func (s *Server) dispatch(req *api.Request) api.Response {
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
	case api.MethodPing:
		return api.Success(req.ID, api.PongResult{
			Type:     "pong",
			Version:  s.version,
			Protocol: api.Protocol,
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
