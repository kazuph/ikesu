package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
)

// Mail/job CLI: send, inbox, msg history|rooms, log, run. The argument
// shapes and output formats mirror herdr's comms.rs.

// sendCLI implements `gtx send`.
func (r *runner) sendCLI(args []string) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		printSendHelp(r)
		return 0
	}
	if len(args) < 2 {
		printSendHelp(r)
		return 2
	}
	to := args[0]
	if to == "*" {
		fmt.Fprintln(r.errOut(), "wildcard recipient '*' is not supported; specify one agent or pane target")
		return 2
	}
	room := dispatch.DefaultRoom
	var from string
	var replyTo *int64
	var text []string
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--room":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --room")
				return 2
			}
			room = v
		case "--from":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --from")
				return 2
			}
			from = v
		case "--reply-to":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --reply-to")
				return 2
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				fmt.Fprintln(r.errOut(), "--reply-to must be a dispatch id")
				return 2
			}
			replyTo = &n
		case "help", "--help", "-h":
			printSendHelp(r)
			return 0
		default:
			text = append(text, args[i])
			i++
		}
	}
	if len(text) == 0 {
		printSendHelp(r)
		return 2
	}
	agent, project, err := r.msgIdentity(from)
	if err != nil {
		fmt.Fprintln(r.errOut(), err.Error())
		return 1
	}
	return r.printMsgSend(api.MsgSendParams{
		Room: room, Project: project, FromAgent: agent,
		To: to, Body: strings.Join(text, " "), ReplyTo: replyTo,
	})
}

// inboxCLI implements `gtx inbox`.
func (r *runner) inboxCLI(args []string) int {
	room := dispatch.DefaultRoom
	var toAgent string
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--room":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --room")
				return 2
			}
			room = v
		case "--to":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --to")
				return 2
			}
			toAgent = v
		case "help", "--help", "-h":
			printMsgHelp(r)
			return 0
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return 2
		}
	}
	if toAgent == "" {
		paneID, err := r.resolveCurrentPaneID()
		if err != nil {
			fmt.Fprintln(r.errOut(), err.Error())
			return 1
		}
		toAgent = paneID
	} else if strings.TrimSpace(toAgent) == "" {
		fmt.Fprintln(r.errOut(), "--to must not be empty")
		return 2
	}
	return r.printMessages("cli:msg:inbox", api.MethodMsgInbox,
		api.MsgInboxParams{Room: room, ToAgent: toAgent}, "inbox")
}

// msgCLI implements `gtx msg <history|rooms>`.
func (r *runner) msgCLI(args []string) int {
	if len(args) == 0 {
		printMsgHelp(r)
		return 2
	}
	switch args[0] {
	case "history":
		room, project, limit, code := r.parseHistoryArgs(args[1:])
		if code >= 0 {
			return code
		}
		return r.printMessages("cli:msg:history", api.MethodMsgHistory,
			api.MsgHistoryParams{Room: room, Project: project, Limit: limit}, "history")
	case "rooms":
		if len(args) != 1 {
			fmt.Fprintln(r.errOut(), "usage: gtx msg rooms")
			return 2
		}
		raw, err := r.request("cli:msg:rooms", api.MethodMsgRooms, api.EmptyParams{})
		if err != nil {
			raw = r.serverNotRunning("cli:msg:rooms")
		}
		var resp api.Response
		if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
			fmt.Fprintln(r.errOut(), string(raw))
			return 1
		}
		var res api.MsgRoomsResult
		if err := json.Unmarshal(resp.Result, &res); err != nil {
			fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
			return 1
		}
		for _, room := range res.Rooms {
			fmt.Fprintln(r.out(), room)
		}
		return 0
	case "help", "--help", "-h":
		printMsgHelp(r)
		return 0
	default:
		printMsgHelp(r)
		return 2
	}
}

func (r *runner) parseHistoryArgs(args []string) (room string, project *string, limit uint32, code int) {
	room = dispatch.DefaultRoom
	limit = 50
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--room":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --room")
				return "", nil, 0, 2
			}
			room = v
		case "--project":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --project")
				return "", nil, 0, 2
			}
			project = &v
		case "--limit":
			v, ok := flagValue(args, &i)
			if !ok {
				fmt.Fprintln(r.errOut(), "missing value for --limit")
				return "", nil, 0, 2
			}
			n, err := parseU32Flag("--limit", v)
			if err != nil {
				fmt.Fprintln(r.errOut(), err.Error())
				return "", nil, 0, 2
			}
			limit = n
		case "help", "--help", "-h":
			printLogHelp(r)
			return "", nil, 0, 0
		default:
			fmt.Fprintf(r.errOut(), "unknown option: %s\n", args[i])
			return "", nil, 0, 2
		}
	}
	return room, project, limit, -1
}

// --- identity helpers ---

// msgIdentity resolves the from_agent and project for msg.send.
func (r *runner) msgIdentity(explicitFrom string) (agent, project string, err error) {
	paneID, perr := r.resolveCurrentPaneID()
	if explicitFrom == "" && perr != nil {
		return "", "", perr
	}
	project = ""
	if perr == nil {
		project = r.paneCwd(paneID)
	}
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = cwd
		}
	}
	if explicitFrom != "" {
		agent = strings.TrimSpace(explicitFrom)
		if agent == "" {
			return "", "", fmt.Errorf("--from/--to must not be empty")
		}
		return agent, project, nil
	}
	name, err := r.resolveAgentName(paneID)
	if err != nil {
		return "", "", err
	}
	return name, project, nil
}

// paneCwd returns a pane's cwd via pane.get ("" when unavailable).
func (r *runner) paneCwd(paneID string) string {
	raw, err := r.request("cli:msg:pane", api.MethodPaneGet, api.PaneTarget{PaneID: paneID})
	if err != nil {
		return ""
	}
	var resp api.Response
	if json.Unmarshal(raw, &resp) != nil || resp.Err != nil {
		return ""
	}
	var res api.PaneInfoResult
	if json.Unmarshal(resp.Result, &res) != nil {
		return ""
	}
	if res.Pane.Cwd == nil {
		return ""
	}
	return *res.Pane.Cwd
}

// --- response printing ---

func (r *runner) printMsgSend(p api.MsgSendParams) int {
	raw, err := r.request("cli:msg:send", api.MethodMsgSend, p)
	if err != nil {
		raw = r.serverNotRunning("cli:msg:send")
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res api.MsgSendResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	fmt.Fprintf(r.out(), "sent %d message(s)\n", len(res.Messages))
	for _, m := range res.Messages {
		printMsgMessage(r.out(), m)
	}
	if len(res.Nudged) > 0 {
		fmt.Fprintf(r.out(), "nudged: %s\n", strings.Join(res.Nudged, ", "))
	}
	return 0
}

func (r *runner) printMessages(id, method string, params any, label string) int {
	raw, err := r.request(id, method, params)
	if err != nil {
		raw = r.serverNotRunning(id)
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var res struct {
		Messages []api.MsgMessage `json:"messages"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		fmt.Fprintf(r.errOut(), "gtx: invalid server response: %v\n", err)
		return 1
	}
	if len(res.Messages) == 0 {
		fmt.Fprintf(r.out(), "%s: no messages\n", label)
		return 0
	}
	for _, m := range res.Messages {
		printMsgMessage(r.out(), m)
	}
	return 0
}
