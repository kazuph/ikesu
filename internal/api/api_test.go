package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseSuccessWireShape(t *testing.T) {
	resp := Success("cli:pane:list", PaneListResult{Type: "pane_list", Panes: []PaneInfo{}})
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.HasPrefix(got, `{"id":"cli:pane:list","result":`) {
		t.Fatalf("unexpected field order: %s", got)
	}
	var decoded Response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "cli:pane:list" || decoded.Err != nil {
		t.Fatalf("decoded response mismatch: %+v", decoded)
	}
}

func TestResponseErrorWireShape(t *testing.T) {
	resp := Failure("cli:pane:get", ErrPaneNotFound, "pane not found: p99")
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"error":{"code":"pane_not_found","message":"pane not found: p99"},"id":"cli:pane:get"}`
	if string(raw) != want {
		t.Fatalf("got %s, want %s", raw, want)
	}
}

func TestRequestResponseRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := Request{
		ID:     "cli:pane:read",
		Method: MethodPaneRead,
		Params: MarshalParams(PaneReadParams{PaneID: "p3", Source: "recent", StripANSI: true}),
	}
	if err := WriteRequest(&buf, req); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != req.ID || got.Method != req.Method {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	var params PaneReadParams
	if err := got.DecodeParams(&params); err != nil {
		t.Fatal(err)
	}
	if params.PaneID != "p3" || params.Source != "recent" || !params.StripANSI {
		t.Fatalf("params mismatch: %+v", params)
	}

	buf.Reset()
	if err := WriteResponse(&buf, Success("cli:pane:read",
		PaneReadResultEnvelope{Type: "pane_read", Read: PaneReadResult{PaneID: "p3", Text: "hi\n"}})); err != nil {
		t.Fatal(err)
	}
	resp, err := ReadResponse(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	var env PaneReadResultEnvelope
	if err := json.Unmarshal(resp.Result, &env); err != nil {
		t.Fatal(err)
	}
	if env.Read.Text != "hi\n" {
		t.Fatalf("unexpected read text: %q", env.Read.Text)
	}
}

func TestEmptyParamsMarshalAsObject(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteRequest(&buf, Request{ID: "cli:ping", Method: MethodPing}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"params":{}`) {
		t.Fatalf("empty params should marshal as object: %s", buf.String())
	}
}
