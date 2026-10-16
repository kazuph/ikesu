package client

import "fmt"

// The existing output channel carries host negotiation separately from frames.
// Remember each endpoint's request so switching machines restores the right one.
func (m *machineClient) keyboardOutput(e *managedEndpoint, data []byte) bool {
	switch string(data) {
	case "\x1b[<1u\x1b[>7u":
		e.hostKbdFlags = 7
	case "\x1b[<1u\x1b[>31u":
		e.hostKbdFlags = 31
	default:
		return false
	}
	if m.endpoints[m.active] == e {
		_ = m.syncKeyboardOutput(e)
	}
	return true
}

func (m *machineClient) syncKeyboardOutput(e *managedEndpoint) error {
	wanted := e.hostKbdFlags
	if wanted == 0 {
		wanted = 7
	}
	if m.hostKbdFlags == 0 {
		m.hostKbdFlags = 7
	}
	if wanted == m.hostKbdFlags {
		return nil
	}
	if _, err := fmt.Fprintf(m.out, "\x1b[<1u\x1b[>%du", wanted); err != nil {
		return err
	}
	m.hostKbdFlags = wanted
	return nil
}
