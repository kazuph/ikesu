package endpoint

import (
	"strings"
	"testing"
)

func TestGraphicsFileReplyMatchesOnlyOneExpectedImage(t *testing.T) {
	for _, tt := range []struct {
		raw              string
		matched, success bool
	}{
		{"\x1b_Gi=17;OK\x1b\\", true, true},
		{"\x1b_Gp=2,i=17,I=3;OK\x1b\\", true, true},
		{"\x1b_Gi=17;ENOENT:file missing\x1b\\", true, false},
		{"\x1b_Gi=17;OK \x1b\\", true, false},
		{"\x1b_Gi=18;OK\x1b\\", false, false},
		{"\x1b_Gi=17,i=17;OK\x1b\\", false, false},
		{"\x1b_Gi=17,q=0;OK\x1b\\", false, false},
		{"\x1b_Gi=17,p=-1;OK\x1b\\", false, false},
		{"\x1b_GI=17;OK\x1b\\", false, false},
		{"\x1b_Gi=17;OK", false, false},
		{"ordinary key input", false, false},
		{"\x1b_Gi=17;" + strings.Repeat("X", 4096) + "\x1b\\", false, false},
	} {
		matched, success := ParseGraphicsReply([]byte(tt.raw), 17)
		if matched != tt.matched || success != tt.success {
			t.Fatalf("reply %q: %v/%v, want %v/%v", tt.raw, matched, success, tt.matched, tt.success)
		}
	}
}
