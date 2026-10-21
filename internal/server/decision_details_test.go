package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestDecisionDescriptionMultiCLIWatchAndStatusRealServer(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	opts := cli.Options{Session: "test", Explicit: true, Version: "test-version"}
	var out, errs bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errs
	asked := make(chan int, 1)
	go func() {
		asked <- cli.Run([]string{"decision", "ask", "--title", "Choose", "--multi-select", "--allow-text", "--option-desc", "one:First detail", "--option", "one:One:other", "--option", "two:Two:other"}, opts)
	}()
	var pending api.DecisionListResult
	waitPromptCondition(t, func() bool {
		apiOK(t, dir, api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	d := pending.Decisions[0]
	if !d.MultiSelect || d.Options[0].Description == nil || *d.Options[0].Description != "First detail" || d.Options[1].Description != nil {
		t.Fatal(d)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	watchOpts := opts
	watchOpts.Stdout = write
	watched := make(chan int, 1)
	go func() { watched <- cli.Run([]string{"decision", "watch"}, watchOpts); write.Close() }()
	rd := bufio.NewReader(read)
	readLine := func() map[string]json.RawMessage {
		t.Helper()
		read.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		t.Log(string(line))
		return value
	}
	readLine()
	created := readLine()
	var streamed api.Decision
	if err := json.Unmarshal(created["decision"], &streamed); err != nil {
		t.Fatal(err)
	}
	if streamed.Options[0].Description == nil || *streamed.Options[0].Description != "First detail" {
		t.Fatal(streamed)
	}
	var answerOut, answerErr bytes.Buffer
	answerOpts := opts
	answerOpts.Stdout = &answerOut
	answerOpts.Stderr = &answerErr
	if code := cli.Run([]string{"decision", "answer", d.DecisionID, "one", "two", "--text", "Additional detail"}, answerOpts); code != 0 {
		t.Fatal(code, answerErr.String())
	}
	if code := <-asked; code != 0 {
		t.Fatal(code, errs.String())
	}
	var resp api.Response
	json.Unmarshal(answerOut.Bytes(), &resp)
	var result api.DecisionResult
	json.Unmarshal(resp.Result, &result)
	if result.Decision.Answer == nil || len(result.Decision.Answer.OptionIDs) != 2 || result.Decision.Answer.Text == nil || *result.Decision.Answer.Text != "Additional detail" || *result.Decision.Answer.OptionID != "one" {
		t.Fatal(result)
	}
	resolved := readLine()
	if err := json.Unmarshal(resolved["decision"], &streamed); err != nil || streamed.Status != "answered" || len(streamed.Answer.OptionIDs) != 2 {
		t.Fatal(streamed, err)
	}
	read.Close()
	select {
	case code := <-watched:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop on pipe close")
	}
	var statusOut, statusErr bytes.Buffer
	statusOpts := opts
	statusOpts.Stdout = &statusOut
	statusOpts.Stderr = &statusErr
	if code := cli.Run([]string{"status", "--json"}, statusOpts); code != 0 {
		t.Fatal(code, statusErr.String())
	}
	var status struct {
		Server struct {
			Running      bool
			Capabilities map[string]bool
		}
	}
	if err := json.Unmarshal(statusOut.Bytes(), &status); err != nil || !status.Server.Running || !status.Server.Capabilities["screen_prompt_decisions"] {
		t.Fatal(status, err)
	}
	t.Log("gtx multi JSON:", answerOut.String())
	t.Log("gtx status JSON:", statusOut.String())
}
