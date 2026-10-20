package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestWindowTitleStandaloneRestartKeepsSessionActualPTY(t *testing.T) {
	for _, name := range []string{"default", "title-demo"} {
		t.Run(name, func(t *testing.T) {
			wrapper := filepath.Join(t.TempDir(), "named-standalone")
			if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %q --session %q \"$@\"\n", binPath, name)), 0700); err != nil {
				t.Fatal(err)
			}
			s := startVersionMode(t, nil, true, wrapper, "")
			var raw strings.Builder
			s.observeOutput = func(b []byte) { raw.Write(b) }
			title := "gtx"
			if name != "default" {
				title += ":" + name
			}
			osc := "\x1b]0;" + title + "\x07"
			s.waitFor("initial title and shell", 10*time.Second, func(tx string) bool {
				return strings.Contains(tx, "❯") && strings.Count(raw.String(), osc) >= 2
			})
			x, y := s.find("[menu]")
			if x < 0 {
				t.Fatal(s.text())
			}
			s.click(x, y)
			s.waitFor("restart menu", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "restart") })
			x, y = s.find("restart")
			s.click(x, y)
			s.waitFor("restart confirmation", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "Restart gtx?") })
			before := raw.Len()
			s.send("\r")
			s.waitFor("restarted title and shell", 15*time.Second, func(tx string) bool {
				return strings.Contains(tx, "❯") && strings.Count(raw.String()[before:], osc) >= 2
			})
			for _, match := range regexp.MustCompile("\x1b\\]0;([^\x07]*)\x07").FindAllStringSubmatch(raw.String()[before:], -1) {
				if match[1] != title && match[1] != "gtx" {
					t.Fatalf("restart changed session title: %q want %q", match[1], title)
				}
			}
			t.Logf("standalone restart: %s -> %s", title, title)
			if out := titleEvidenceDir; out != "" {
				if err := os.MkdirAll(out, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, "restart-"+name+".raw"), []byte(raw.String()), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
