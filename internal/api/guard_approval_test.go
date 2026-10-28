package api

import (
	"encoding/json"
	"testing"
)

func TestRequestPeerIdentityCannotComeFromJSON(t *testing.T) {
	var request Request
	if err := json.Unmarshal([]byte(`{"id":"fake","method":"decision.create","peer_pid":123,"PeerPID":456,"params":{}}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.PeerPID != 0 {
		t.Fatal("client supplied kernel identity", request.PeerPID)
	}
}
