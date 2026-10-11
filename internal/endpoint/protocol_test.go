package endpoint

import (
	"strings"
	"testing"
)

func TestGenerationRange(t *testing.T) {
	old := Current()
	new := Current()
	new.Generation = 2
	for _, peers := range [][2]Peer{{new, old}, {old, new}, {new, new}} {
		g, err := Negotiate(peers[0], peers[1])
		if err != nil || g != min(peers[0].Generation, peers[1].Generation) {
			t.Fatal(g, err)
		}
	}
	if !Allows(1, 1, old.Methods, "input") || Allows(1, 2, []string{"machine.route"}, "machine.route") || Allows(2, 1, old.Methods, "machine.route") {
		t.Fatal("capability selection")
	}
}

func TestGenerationExplicitRejection(t *testing.T) {
	for _, test := range []struct {
		client, server Peer
		code           string
	}{
		{Current(), Peer{Protocol: ProtocolVersion}, "endpoint_generation_unsupported"},
		{Peer{Protocol: ProtocolVersion}, Current(), "endpoint_generation_unsupported"},
		{Current(), Peer{Protocol: ProtocolVersion, Generation: 2, MinGeneration: 2}, "endpoint_generation_unsupported"},
		{Peer{Protocol: ProtocolVersion, Generation: 2, MinGeneration: 2}, Current(), "endpoint_generation_newer"},
		{Peer{Protocol: 99, Generation: 1, MinGeneration: 1}, Current(), "protocol_unsupported"},
	} {
		_, err := Negotiate(test.client, test.server)
		if err == nil || !strings.HasPrefix(err.Error(), test.code+":") {
			t.Fatal(test, err)
		}
	}
}
