package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/snapshot"
)

const paneSnapshotUsage = "usage: ikesu pane snapshot [<pane>] [--last-turn|--handoff] [--lines N] [--max-chars N] [--save-log] [--json] [--copy] [--send <pane> [--room R]]\n  --save-log is only supported in handoff mode"

func (r *runner) paneSnapshot(args []string) int {
	var params api.PaneSnapshotParams
	var lastTurn, handoff, asJSON, copyText, roomSet bool
	to, room := "", dispatch.DefaultRoom
	for i := 0; i < len(args); {
		switch arg := args[i]; arg {
		case "help", "-h", "--help":
			fmt.Fprintln(r.out(), paneSnapshotUsage)
			return 0
		case "--last-turn":
			lastTurn = true
			i++
		case "--handoff":
			handoff = true
			i++
		case "--save-log":
			params.SaveLog = true
			i++
		case "--json":
			asJSON = true
			i++
		case "--copy":
			copyText = true
			i++
		case "--lines", "--max-chars":
			value, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "missing value for "+arg)
			}
			n, err := parseU32Flag(arg, value)
			if err != nil {
				return usageErr(r, err.Error())
			}
			if arg == "--lines" {
				params.Lines = &n
			} else {
				params.MaxChars = &n
			}
		case "--send", "--room":
			value, ok := flagValue(args, &i)
			if !ok || value == "" || strings.HasPrefix(value, "--") {
				return usageErr(r, "missing value for "+arg)
			}
			if arg == "--send" {
				to = value
			} else {
				room, roomSet = value, true
			}
		default:
			if params.PaneID != "" || strings.HasPrefix(arg, "-") {
				return usageErr(r, paneSnapshotUsage)
			}
			params.PaneID = arg
			i++
		}
	}
	if lastTurn && params.SaveLog {
		return usageErr(r, "--save-log is only supported in handoff mode")
	}
	if lastTurn && handoff {
		return usageErr(r, "--last-turn and --handoff are mutually exclusive")
	}
	if roomSet && to == "" {
		return usageErr(r, "--room requires --send")
	}
	if to == "*" {
		return usageErr(r, "wildcard recipient '*' is not supported; specify one agent or pane target")
	}

	params.Mode = "handoff"
	if lastTurn {
		params.Mode = "last_turn"
	}
	if params.PaneID == "" {
		pane, err := r.resolveCurrentPaneID()
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		params.PaneID = pane
	}
	const requestID = "cli:pane:snapshot"
	raw, err := r.request(requestID, api.MethodPaneSnapshot, params)
	if err != nil {
		raw = r.serverNotRunning(requestID)
	}
	var response api.Response
	if err := json.Unmarshal(raw, &response); err != nil || response.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var result api.PaneSnapshotResult[snapshot.Result]
	if err := json.Unmarshal(response.Result, &result); err != nil || result.Type != "pane_snapshot" {
		fmt.Fprintln(r.errOut(), "ikesu: invalid pane snapshot response")
		return 1
	}
	return r.outputSnapshot(result.Snapshot, asJSON, copyText, to, room)
}

func (r *runner) outputSnapshot(result snapshot.Result, asJSON, copyText bool, to, room string) int {
	if asJSON {
		if err := json.NewEncoder(r.out()).Encode(result); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
	} else if !copyText && to == "" {
		fmt.Fprintln(r.out(), result.Markdown)
	}
	statusOut := r.out()
	if asJSON {
		statusOut = r.errOut()
	}
	code := 0
	if copyText {
		var tty io.Writer
		if file, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			defer file.Close()
			tty = file
		}
		if err := snapshot.Copy(result.Markdown, r.o.getenv, exec.LookPath, tty); err != nil {
			fmt.Fprintln(r.errOut(), err)
			code = 1
		} else {
			fmt.Fprintf(statusOut, "copied %d chars\n", utf8.RuneCountInString(result.Markdown))
		}
	}
	if to != "" {
		agent, project, err := r.msgIdentity("")
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if sendCode := r.printMsgSendTo(statusOut, api.MsgSendParams{
			Room: room, Project: project, FromAgent: agent, To: to, Body: result.Markdown,
		}); sendCode != 0 {
			return sendCode
		}
	}
	return code
}
