package server

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/endpoint"
)

func (s *Server) decisionProjection() (*endpoint.DecisionProjection, error) {
	s.initDecisionHub()
	if s.decisions.boot == "" {
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, err
		}
		s.decisions.boot = hex.EncodeToString(bytes[:])
		s.decisions.revision = 1
		if dispatch.DBExists(s.session) {
			store, err := s.db()
			if err != nil {
				return nil, err
			}
			if err := s.expireDecisions(store); err != nil {
				return nil, err
			}
		}
	}
	pending := []api.Decision{}
	if s.store != nil {
		var err error
		pending, err = s.store.Decisions("pending")
		if err != nil {
			return nil, err
		}
	}
	return &endpoint.DecisionProjection{BootID: s.decisions.boot, Revision: s.decisions.revision, Pending: pending}, nil
}
func (s *Server) sendDecisionProjection(c *clientConn) {
	if c.available < endpoint.DecisionGeneration {
		return
	}
	projection, err := s.decisionProjection()
	if err != nil {
		_ = c.send(clientMsg{Type: "error", Reason: "decision projection: " + err.Error()})
		return
	}
	if err := c.send(clientMsg{Type: "decisions", Decisions: projection}); err != nil {
		s.dropConn(c)
	}
}
func (s *Server) pushDecisionProjection() {
	s.decisions.revision++
	for _, c := range s.displayClients() {
		s.sendDecisionProjection(c)
	}
}
