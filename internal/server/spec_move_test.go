package server

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/teststate"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
)

func TestSPECMoveAllDestinationsViaSocketCLIAndEvents(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	moving := splitRight(t, dir).Pane.PaneID
	var original api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: moving}, &original)
	baseTab := original.Pane.TabID
	conn, rd := subscribe(t, dir, []string{"pane.moved"})
	defer conn.Close()
	for _, transport := range []string{"socket", "CLI"} {
		for _, dest := range []string{"new_tab", "new_workspace", "tab"} {
			var result api.PaneMoveEnvelope
			if transport == "socket" {
				params := api.PaneMoveParams{PaneID: moving, Focus: true, Destination: api.PaneMoveDestination{Type: dest}}
				if dest == "tab" {
					params.Destination.TabID = baseTab
					params.Destination.Split = strp("right")
				}
				apiOK(t, dir, api.MethodPaneMove, params, &result)
			} else {
				args := []string{"pane", "move", moving, "--focus"}
				switch dest {
				case "new_tab":
					args = append(args, "--new-tab")
				case "new_workspace":
					args = append(args, "--new-workspace")
				case "tab":
					args = append(args, "--tab", baseTab, "--split", "right")
				}
				var out strings.Builder
				if code := cli.Run(args, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &out}); code != 0 {
					t.Fatal(out.String())
				}
				var response api.Response
				if err := json.Unmarshal([]byte(out.String()), &response); err != nil {
					t.Fatal(err)
				}
				if response.Err != nil {
					t.Fatal(response.Err)
				}
				if err := json.Unmarshal(response.Result, &result); err != nil {
					t.Fatal(err)
				}
			}
			if !result.MoveResult.Changed || result.MoveResult.Pane.PaneID != moving || result.MoveResult.Pane.TerminalID != original.Pane.TerminalID || result.MoveResult.FocusedPaneID != moving {
				t.Fatalf("%s %s: %+v", transport, dest, result)
			}
			for {
				line, err := rd.ReadBytes('\n')
				if err != nil {
					t.Fatal("missing moved event", transport, dest, err)
				}
				var envelope api.EventEnvelope
				if err = json.Unmarshal(line, &envelope); err != nil {
					t.Fatal(err)
				}
				var event struct {
					Event          string       `json:"type"`
					Pane           api.PaneInfo `json:"pane"`
					PreviousPaneID string       `json:"previous_pane_id"`
				}
				if err = json.Unmarshal(envelope.Data, &event); err != nil {
					t.Fatal(err)
				}
				if event.Event == "pane_moved" {
					if event.Pane.PaneID != moving || event.PreviousPaneID != moving || event.Pane.TerminalID != original.Pane.TerminalID {
						t.Fatal("event target identity", event)
					}
					break
				}
			}
		}
	}
}
