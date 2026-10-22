package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"runtime"
	"testing"
	"time"
)

func TestFossilExpiryAndAuthorityClearing(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "IKESU_STATE_DIR", teststate.TempDir(t))
	teststate.Setenv(t, "SHELL", "/bin/sh")
	// The fixture types UTF-8 into a real interactive shell. macOS /bin/sh
	// treats high bytes as readline commands in the default C locale.
	locale := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		locale = "en_US.UTF-8"
	}
	teststate.Setenv(t, "LC_ALL", locale)

	dir, done := startServerAt(t, "fossil")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); waitServerExit(t, done) }()

	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right", Focus: true}, nil)

	var ok api.OkResult
	// p1: fossil spinner screen, agent reported with an empty state so
	// the tracker verdict shows. p2: reported working + idle screen.
	var ok2 api.OkResult
	sess1, sess2 := "fossil-sess", "auth-sess"
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{
		PaneID: "p1", Source: "test", Agent: "claude", State: "", SessionID: &sess1,
	}, &ok)
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{
		PaneID: "p2", Source: "test", Agent: "claude", State: "working", SessionID: &sess2,
	}, &ok2)

	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: "p1",
		Text:   "printf '────────\\n✻ Thinking… (12s · esc to interrupt)\\n────────\\n❯ \\n────────\\n'",
		Keys:   []string{"enter"},
	}, nil)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: "p2",
		Text:   "printf '────────\\n❯ \\n────────\\n'",
		Keys:   []string{"enter"},
	}, nil)

	status := func(paneID string) string {
		var got api.PaneInfoResult
		apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: paneID}, &got)
		return got.Pane.AgentStatus
	}

	// p1 should reach "working" first (spinner on screen), p2 stays
	// "working" via the reported authority.
	deadline := time.Now().Add(8 * time.Second)
	start := time.Now()
	last := ""
	for time.Now().Before(deadline) && status("p1") != "working" {
		if s := status("p1"); s != last {
			t.Logf("%.1fs p1 %q", time.Since(start).Seconds(), s)
			last = s
		}
		time.Sleep(200 * time.Millisecond)
	}
	if s1 := status("p1"); s1 != "working" {
		var rd api.PaneReadResultEnvelope
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: "p1"}, &rd)
		t.Logf("p1 screen:\n%s", rd.Read.Text)
		t.Fatalf("p1 fossil screen should show working first, got %q", s1)
	}
	t.Logf("%.1fs p1 working", time.Since(start).Seconds())

	// ~15s later: both panes idle — fossil expired, stale authority
	// cleared.
	deadline = time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if status("p1") == "idle" && status("p2") == "idle" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("fossil/authority expiry did not settle to idle: p1=%q p2=%q",
		status("p1"), status("p2"))
}
