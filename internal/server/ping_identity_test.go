package server

import (
	"encoding/json"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

func TestPingIncludesServerIdentity(t *testing.T) {
	s := &Server{version: "test-version", executable: "/test/ikesu", generation: "test-generation"}
	response := s.dispatch(&api.Request{ID: "ping", Method: api.MethodPing})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	var pong api.PongResult
	if err := json.Unmarshal(response.Result, &pong); err != nil {
		t.Fatal(err)
	}
	if pong.Type != "pong" || pong.Version != s.version || pong.Executable != s.executable || pong.Generation != s.generation {
		t.Fatalf("unexpected ping: %+v", pong)
	}
}
