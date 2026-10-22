package server

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPreviewAbortNeverWritesRestorationToRealPTY(t *testing.T) {
	for _, reason := range []string{"disappeared", "question-changed", "unreadable", "cursor-mismatch", "external-input"} {
		t.Run(reason, func(t *testing.T) {
			teststate.Lock(t)
			s, p, input := screenTerminal(t, "claude-preview", "claude")
			scanScreen(s)
			waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "\x1b[B" })
			picture := screenDialog(t, "claude-preview")
			switch reason {
			case "disappeared":
				picture = "ordinary idle screen"
			case "question-changed":
				picture = strings.ReplaceAll(picture, "Which draft should we keep?", "Which new draft should we keep?")
			case "unreadable":
				picture = strings.ReplaceAll(picture, "└──────────────────────────────────────────┘", "")
			case "external-input":
				p.RecordExternalInput()
			}
			paintScreen(p, picture)
			s.app.Do(func() {
				w := s.screenPrompts.watches[1]
				if w == nil || w.preview == nil {
					t.Error("missing active scan")
					return
				}
				w.preview.lastStep = time.Time{}
				s.scanScreenPrompts()
				if w.preview != nil {
					t.Error("unsafe scan retained")
				}
			})
			if data, _ := os.ReadFile(input); string(data) != "\x1b[B" {
				t.Fatalf("extra bytes after %s: %q", reason, data)
			}
		})
	}
}
