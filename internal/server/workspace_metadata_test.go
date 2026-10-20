package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestG2WorkspaceMetadataSequenceSourcesClearExpiryAndLimits(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	var list api.WorkspaceListResult
	apiOK(t, dir, "workspace.list", api.EmptyParams{}, &list)
	wid := list.Workspaces[0].WorkspaceID
	report := func(source string, tokens map[string]any, extra map[string]any) api.Response {
		p := map[string]any{"workspace_id": wid, "source": source, "tokens": tokens}
		for k, v := range extra {
			p[k] = v
		}
		return apiReq(t, dir, "workspace.report_metadata", p)
	}
	ok := func(r api.Response) {
		t.Helper()
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	tokens := func() map[string]string {
		var info api.WorkspaceInfoResult
		apiOK(t, dir, "workspace.get", api.WorkspaceTarget{WorkspaceID: wid}, &info)
		return info.Workspace.Tokens
	}
	ok(report("one", map[string]any{"custom": "first"}, map[string]any{"seq": 2}))
	for _, seq := range []int{1, 2} {
		ok(report("one", map[string]any{"custom": "stale"}, map[string]any{"seq": seq}))
		if tokens()["custom"] != "first" {
			t.Fatal("stale/equal sequence changed token", tokens())
		}
	}
	ok(report("two", map[string]any{"custom": "second"}, nil))
	if tokens()["custom"] != "second" {
		t.Fatal(tokens())
	}
	ok(report("one", map[string]any{"custom": nil}, map[string]any{"seq": 3}))
	if len(tokens()) != 0 {
		t.Fatal("clear must remove latest token across sources", tokens())
	}
	ok(report("one", map[string]any{"custom": "old"}, nil))
	ok(report("two", map[string]any{"custom": "expiring"}, map[string]any{"ttl_ms": 1}))
	time.Sleep(3 * time.Millisecond)
	if len(tokens()) != 0 {
		t.Fatal("expired value must not restore previous source", tokens())
	}
	for _, p := range []map[string]any{{"ttl_ms": 0}, {"ttl_ms": 86400001}} {
		if report("one", map[string]any{"valid": "x"}, p).Err == nil {
			t.Fatal("invalid TTL accepted")
		}
	}
	if report("one", map[string]any{"invalid.name": "x"}, nil).Err == nil {
		t.Fatal("invalid name accepted")
	}
	many := map[string]any{}
	for i := 0; i < 17; i++ {
		many[string(rune('a'+i))] = "v"
	}
	if report("one", many, nil).Err == nil {
		t.Fatal("17 tokens accepted")
	}
	delete(many, "q")
	ok(report("one", many, nil))
	other := map[string]any{}
	for i := 0; i < 16; i++ {
		other["other_"+string(rune('a'+i))] = "v"
	}
	ok(report("two", other, nil))
	if report("three", map[string]any{"overflow": "v"}, nil).Err == nil {
		t.Fatal("33 workspace tokens accepted")
	}
}
