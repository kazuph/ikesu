// Package endpoint defines the display transport independently of the API.
package endpoint

import "fmt"

const ProtocolVersion = 1
const Generation = 4
const NotificationGeneration = 4
const MinGeneration = 1
const MachineGeneration = 2

type Peer struct {
	Protocol      int      `json:"protocol"`
	Generation    int      `json:"generation"`
	MinGeneration int      `json:"min_generation"`
	Methods       []string `json:"methods,omitempty"`
	SurfaceCodecs []string `json:"surface_codecs,omitempty"`
}

func GraphicsPeer() Peer {
	peer := Current()
	peer.SurfaceCodecs = []string{GraphicsPatchCodec}
	peer.Methods = append(peer.Methods, "control")
	return peer
}

func HasGraphicsPatches(peer Peer) bool {
	for _, codec := range peer.SurfaceCodecs {
		if codec == GraphicsPatchCodec {
			return true
		}
	}
	return false
}

func Current() Peer {
	return Peer{Protocol: ProtocolVersion, Generation: Generation, MinGeneration: MinGeneration,
		Methods: []string{"input", "resize", "rendered", "preferences_error", "config_result", "visibility", "workspace_select", "view_config", "machine_menu", "decision_answer"}}
}

// MachinePeer refuses an older server before it can create a visible viewer.
func MachinePeer() Peer {
	peer := Current()
	peer.MinGeneration = MachineGeneration
	return peer
}

func RequiredGeneration(method string) int {
	switch method {
	case "decision_answer":
		return DecisionGeneration
	case "visibility", "workspace_select", "view_config", "machine_menu":
		return MachineGeneration
	default:
		return 1
	}
}

func Negotiate(client, server Peer) (int, error) {
	if client.Protocol != server.Protocol {
		return 0, fmt.Errorf("protocol_unsupported: display protocol %d requires %d", client.Protocol, server.Protocol)
	}
	if client.Generation < 1 || client.MinGeneration < 1 || client.MinGeneration > client.Generation {
		return 0, fmt.Errorf("endpoint_generation_unsupported: upgrade the display client to endpoint generation 1 or later")
	}
	if server.Generation < 1 || server.MinGeneration < 1 || server.MinGeneration > server.Generation {
		return 0, fmt.Errorf("endpoint_generation_unsupported: upgrade the server to endpoint generation 1 or later")
	}
	if client.Generation < server.MinGeneration {
		return 0, fmt.Errorf("endpoint_generation_unsupported: server requires generation %d", server.MinGeneration)
	}
	if server.Generation < client.MinGeneration {
		return 0, fmt.Errorf("endpoint_generation_newer: client requires generation %d", client.MinGeneration)
	}
	return min(client.Generation, server.Generation), nil
}

func Allows(available, required int, methods []string, method string) bool {
	if available < required {
		return false
	}
	for _, name := range methods {
		if name == method {
			return true
		}
	}
	return false
}
