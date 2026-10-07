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
	default:
		var resp api.Response
		s.app.Do(func() {
			resp = s.dispatchOnLoop(req)
		})
		return resp
	}
}
