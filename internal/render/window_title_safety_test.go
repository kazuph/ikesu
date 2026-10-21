package render

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode"
)

func TestWindowTitleSessionNamesAndPlain(t *testing.T) {
	for _, mode := range []string{"summary", "plain"} {
		for _, session := range []string{"default", "coding", "日本語🐱", "demo\a\x1b[2J\n\r\x7f\u009c"} {
			in := titleInput("working")
			in.SidebarConfig.UI.WindowTitle = mode
			got, want := WindowTitle(in, session, nil), "ikesu"
			if session != "default" {
				want += ":" + session
				if strings.HasPrefix(session, "demo") {
					want = "ikesu:demo\\u0007\\u001B[2J\\u000A\\u000D\\u007F\\u009C"
				}
			}
			if mode == "summary" {
				want = "🟡 " + want
			}

			if got != want {
				t.Fatalf("%s session=%q got=%q want=%q", mode, session, got, want)
			}
		}
	}
}

func TestWindowTitleEscapesAllSessionControlsInOSCBytes(t *testing.T) {
	for r := rune(0); r <= 0x9f; r++ {
		if !unicode.IsControl(r) {
			continue
		}
		in := titleInput("blocked")
		var writer WindowTitleState
		var out bytes.Buffer
		if err := writer.Painted(&out, in, "a"+string(r)+"b", nil); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("\x1b]0;🔴 ikesu:a\\u%04Xb\x07", r)
		if out.String() != want {
			t.Fatalf("control U+%04X: got=%q want=%q", r, out.String(), want)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(out.String(), "\x1b]0;"), "\x07")
		if strings.ContainsFunc(body, unicode.IsControl) {
			t.Fatalf("control in OSC body: %q", body)
		}
	}
}

func TestWindowTitleConfigModeChangesWriteOnce(t *testing.T) {
	in := titleInput("working")
	var title WindowTitleState
	var out bytes.Buffer
	for _, step := range []struct{ mode, want string }{
		{"summary", "🟡 ikesu:coding"}, {"summary", "🟡 ikesu:coding"}, // activation
		{"summary", ""}, {"plain", "ikesu:coding"}, {"plain", ""},
		{"summary", "🟡 ikesu:coding"}, {"summary", ""},
	} {
		in.SidebarConfig.UI.WindowTitle = step.mode
		out.Reset()
		if err := title.Painted(&out, in, "coding", nil); err != nil {
			t.Fatal(err)
		}
		want := ""
		if step.want != "" {
			want = "\x1b]0;" + step.want + "\x07"
		}
		if out.String() != want {
			t.Fatalf("mode=%s bytes=%q want=%q", step.mode, out.String(), want)
		}
	}
}
