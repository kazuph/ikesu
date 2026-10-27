package vt

import (
	"bytes"
	"testing"
)

func TestDeviceStatusReports(t *testing.T) {
	for _, tc := range []struct {
		name, query, want string
	}{
		{"operating status", "\x1b[5n", "\x1b[0n"},
		{"cursor position", "\x1b[6n", "\x1b[3;7R"},
		{"DEC cursor position", "\x1b[?6n", "\x1b[?3;7R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestTerminal(t, 80, 24)
			defer e.Close()
			if _, err := e.WriteString("\x1b[3;7H"); err != nil {
				t.Fatal(err)
			}
			ch := expectResponse(e)
			if _, err := e.WriteString(tc.query); err != nil {
				t.Fatal(err)
			}
			if got := waitResponse(t, ch); !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("response to %q = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}
