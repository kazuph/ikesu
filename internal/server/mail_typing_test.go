package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"

	"github.com/kazuph/ikesu/internal/runtime"
)

func TestMailWriteWaitsForRecentExternalInput(t *testing.T) {
	sup := runtime.NewSupervisor()
	pane, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Kill(1)
	s := &Server{sup: sup}
	pane.RecordKeyInput()
	written, _ := s.deliverMailWrite(mailWrite{pane: 1, owner: pane, payload: []byte("interrupted message\n")})
	if written {
		t.Fatal("mail was written to the PTY while external input was recent")
	}
}

// This test must wait for the agreed ten-second quiet interval using a real PTY.
func TestMailAndJobResumeAfterTypingWithoutStatusChange(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)
	pane := firstPaneID(t, dir)
	capture := filepath.Join(t.TempDir(), "pty-input")
	command := fmt.Sprintf("stty raw -echo; printf 'CAPTURE_%%s\\n' READY; exec cat > %q", capture)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pane, Text: command, Keys: []string{"Enter"}}, nil)
	waitRead(t, dir, pane, "CAPTURE_READY")
	reportAgent(t, dir, pane, "codex", "")
	apiOK(t, dir, api.MethodPaneSendText, api.PaneSendTextParams{PaneID: pane, Text: "# human draft "}, nil)
	apiOK(t, dir, api.MethodMsgSend, api.MsgSendParams{Room: "typing", FromAgent: "sender", To: pane, Body: "# first-notice"}, nil)
	apiOK(t, dir, api.MethodMsgSend, api.MsgSendParams{Room: "typing", FromAgent: "sender", To: pane, Body: "# second-notice"}, nil)
	var started api.RunStartedResult
	apiOK(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: pane, Label: "typing-job", Argv: []string{"/usr/bin/true"}}, &started)
	store := openStore(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		jobs, err := store.PendingJobMessages()
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job completion did not queue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	regular, err := store.PendingForAgentCreationOrder(pane)
	if err != nil || len(regular) != 2 {
		t.Fatalf("regular pending=%v err=%v", regular, err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		regular, err = store.PendingForAgentCreationOrder(pane)
		if err != nil {
			t.Fatal(err)
		}
		jobs, err := store.PendingJobMessages()
		if err != nil {
			t.Fatal(err)
		}
		if len(regular) == 0 && len(jobs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not resumed: regular=%v jobs=%v", regular, jobs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	history, err := store.History("typing", nil, 10)
	if err != nil || len(history) != 2 || history[0].Body != "# first-notice" || history[1].Body != "# second-notice" || history[0].DeliveredAt == nil || history[1].DeliveredAt == nil {
		t.Fatalf("history=%v err=%v", history, err)
	}
	deadline = time.Now().Add(5 * time.Second)
	var text string
	for {
		data, err := os.ReadFile(capture)
		if err == nil {
			text = string(data)
		}
		if strings.Contains(text, "typing-job") && strings.Contains(text, "second-notice") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PTY capture incomplete: %q", text)
		}
		time.Sleep(10 * time.Millisecond)
	}
	first, second := strings.Index(text, "first-notice"), strings.Index(text, "second-notice")
	if strings.Count(text, "first-notice") != 1 || strings.Count(text, "second-notice") != 1 || strings.Count(text, "typing-job") != 1 || first >= second {
		t.Fatalf("mail repeated or out of order: %q", text)
	}
}
