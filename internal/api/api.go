// Package api defines the newline-delimited JSON protocol spoken over
// <session-dir>/gtx.sock between the gtx server and its CLI. Method names,
// parameter fields, and result envelopes mirror the herdr subset documented
// in docs/ref/herdr-api-schema.json.
package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Protocol is the gtx wire protocol version. It is independent of herdr's
// protocol number; only the documented subset of methods is implemented.
const Protocol = 1

// Method names implemented by the server.
const (
	MethodPing                    = "ping"
	MethodServerStop              = "server.stop"
	MethodServerRestart           = "server.restart"
	MethodServerLiveHandoff       = "server.live_handoff"
	MethodWorkspaceList           = "workspace.list"
	MethodWorkspaceCreate         = "workspace.create"
	MethodWorkspaceGet            = "workspace.get"
	MethodWorkspaceFocus          = "workspace.focus"
	MethodWorkspaceRename         = "workspace.rename"
	MethodWorkspaceClose          = "workspace.close"
	MethodWorkspaceDuplicate      = "workspace.duplicate"
	MethodWorkspaceSetSection     = "workspace.set_section"
	MethodWorkspaceReportMetadata = "workspace.report_metadata"
	MethodTabList                 = "tab.list"
	MethodTabCreate               = "tab.create"
	MethodTabGet                  = "tab.get"
	MethodTabFocus                = "tab.focus"
	MethodTabRename               = "tab.rename"
	MethodTabClose                = "tab.close"
	MethodPaneList                = "pane.list"
	MethodPaneCurrent             = "pane.current"
	MethodPaneGet                 = "pane.get"
	MethodPaneFocus               = "pane.focus"
	MethodPaneRename              = "pane.rename"
	MethodPaneSplit               = "pane.split"
	MethodPaneClose               = "pane.close"
	MethodPaneRead                = "pane.read"
	MethodPaneSendText            = "pane.send_text"
	MethodPaneSendKeys            = "pane.send_keys"
	MethodPaneSendInput           = "pane.send_input"
	MethodPaneReportAgent         = "pane.report_agent"
	MethodPaneLayout              = "pane.layout"
	MethodPaneNeighbor            = "pane.neighbor"
	MethodPaneEdges               = "pane.edges"
	MethodPaneFocusDir            = "pane.focus_direction"
	MethodPaneResize              = "pane.resize"
	MethodPaneSwap                = "pane.swap"
	MethodPaneZoom                = "pane.zoom"
	MethodPaneMove                = "pane.move"
	MethodPaneArrange             = "pane.arrange"
	MethodPaneScroll              = "pane.scroll"
	MethodPaneCopyMotion          = "pane.copy_motion"
	MethodPaneCopySearch          = "pane.copy_search"
	MethodLayoutExport            = "layout.export"
	MethodLayoutApply             = "layout.apply"
	MethodLayoutSetSplitRatio     = "layout.set_split_ratio"
	MethodEventsSubscribe         = "events.subscribe"
	MethodEventsWait              = "events.wait"
	MethodPaneWaitForOutput       = "pane.wait_for_output"
	MethodMsgSend                 = "msg.send"
	MethodMsgInbox                = "msg.inbox"
	MethodMsgHistory              = "msg.history"
	MethodMsgRooms                = "msg.rooms"
	MethodRunStart                = "run.start"
	MethodRunCancel               = "run.cancel"
	MethodAgentList               = "agent.list"
	MethodAgentGet                = "agent.get"
	MethodAgentRead               = "agent.read"
	MethodAgentSend               = "agent.send"
	MethodAgentPrompt             = "agent.prompt"
	MethodAgentRename             = "agent.rename"
	MethodAgentFocus              = "agent.focus"
	MethodAgentStart              = "agent.start"
	MethodAgentRestore            = "agent.restore"
)

// Error codes emitted by the server or synthesized by the CLI when the
// server cannot be reached.
const (
	ErrServerNotRunning  = "server_not_running"
	ErrInvalidRequest    = "invalid_request"
	ErrUnknownMethod     = "unknown_method"
	ErrPaneNotFound      = "pane_not_found"
	ErrWorkspaceNotFound = "workspace_not_found"
	ErrTabNotFound       = "tab_not_found"
	ErrTabCloseFailed    = "tab_close_failed"
	ErrInvalidKey        = "invalid_key"
	ErrPaneSendFailed    = "pane_send_failed"
	ErrMsgWildcard       = "msg_wildcard_recipient"
	ErrMsgStore          = "msg_store_error"
	ErrInvalidMsgRoom    = "invalid_msg_room"
	ErrInvalidMsgAgent   = "invalid_msg_agent"
	ErrInvalidMsgBody    = "invalid_msg_body"
	ErrMsgNudgeFailed    = "msg_nudge_failed"
	ErrAgentNotFound     = "agent_not_found"
	ErrAmbiguous         = "ambiguous_target"
	ErrAgentNotReady     = "agent_not_ready"
	ErrJobNotFound       = "job_not_found"
	ErrJobStore          = "job_store_error"
	ErrUnsupportedWait   = "unsupported_event_wait_match"
	ErrTimeout           = "timeout"
	ErrSplitNotFound     = "split_not_found"
	ErrStaleContent      = "stale_content"
	ErrCopyMotion        = "copy_motion_unavailable"
)

// Error is the API error body: {"code": ..., "message": ...}.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Request is one line on the API socket:
//
//	{"id": "...", "method": "...", "params": {...}}
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// MarshalParams encodes params for a request; nil becomes {}.
func MarshalParams(params any) json.RawMessage {
	if params == nil {
		return json.RawMessage(`{}`)
	}
	b, err := json.Marshal(params)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{}`)
	}
	return b
}

// Response is the reply to a Request. Success serializes as
// {"id": ..., "result": {...}}; errors serialize as
// {"error": {...}, "id": ...}, matching herdr's field order.
type Response struct {
	ID     string          `json:"-"`
	Result json.RawMessage `json:"-"`
	Err    *Error          `json:"-"`
}

// Success builds a success response carrying result.
func Success(id string, result any) Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return Failure(id, ErrInvalidRequest, err.Error())
	}
	return Response{ID: id, Result: raw}
}

// Failure builds an error response.
func Failure(id, code, message string) Response {
	return Response{ID: id, Err: &Error{Code: code, Message: message}}
}

func (r Response) MarshalJSON() ([]byte, error) {
	if r.Err != nil {
		return json.Marshal(struct {
			Error *Error `json:"error"`
			ID    string `json:"id"`
		}{r.Err, r.ID})
	}
	return json.Marshal(struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
	}{r.ID, r.Result})
}

func (r *Response) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.ID = wire.ID
	r.Result = wire.Result
	r.Err = wire.Error
	return nil
}

// DecodeParams unmarshals the request params into out.
func (r *Request) DecodeParams(out any) error {
	if len(r.Params) == 0 || string(r.Params) == "null" {
		return nil
	}
	return json.Unmarshal(r.Params, out)
}

const maxLine = 4 << 20

// ReadRequest reads one newline-delimited request.
func ReadRequest(r *bufio.Reader) (*Request, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("invalid request JSON: %w", err)
	}
	return &req, nil
}

// ReadResponse reads one newline-delimited response.
func ReadResponse(r *bufio.Reader) (*Response, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("invalid response JSON: %w", err)
	}
	return &resp, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		if buf.Len() > maxLine {
			return nil, fmt.Errorf("message exceeds %d bytes", maxLine)
		}
		if !isPrefix {
			break
		}
	}
	return buf.Bytes(), nil
}

// WriteRequest writes one request line.
func WriteRequest(w io.Writer, req Request) error {
	if req.Params == nil {
		req.Params = json.RawMessage(`{}`)
	}
	return json.NewEncoder(w).Encode(req)
}

// WriteResponse writes one response line.
func WriteResponse(w io.Writer, resp Response) error {
	return json.NewEncoder(w).Encode(resp)
}
