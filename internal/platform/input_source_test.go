package platform

import (
	"runtime"
	"strings"
	"testing"
)

func TestPrefixInputSourceSwitchAndRestoreInvocation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		return
	}
	var calls [][]string
	s := InputSource{Run: func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("com.apple.keylayout.test\n"), nil
	}}
	for _, p := range []bool{true, true, false, false} {
		if err := s.Prefix(p, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 2 || !strings.Contains(calls[0][4], "TISCopyCurrentASCIICapable") || calls[1][5] != "com.apple.keylayout.test" {
		t.Fatal(calls)
	}
	if err := s.Prefix(true, false); err != nil || len(calls) != 2 {
		t.Fatal(err, calls)
	}
}
