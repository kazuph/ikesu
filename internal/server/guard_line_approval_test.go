package server

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
)

func claimOfString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// startOwner は、子プロセス（peer）を持つ親（owner）を起動する。peer は owner の子孫として本物のプロセス。
func startOwner(t *testing.T) (owner *exec.Cmd, peer int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 120; true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
		if err == nil {
			if pid, err := strconv.Atoi(strings.Fields(string(out))[0]); err == nil {
				return cmd, pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no child process")
	return nil, 0
}

// 試験のバイナリ自身を peer にするための子プロセス。
func TestGuardLineHelperProcess(t *testing.T) {
	if os.Getenv("GUARD_LINE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(120 * time.Second)
}

// startOwnerWithTestPeer は、owner（sh）の子として試験のバイナリのプロセス（peer）を起動する。
func startOwnerWithTestPeer(t *testing.T) (owner *exec.Cmd, peer int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "\"$1\" -test.run=TestGuardLineHelperProcess; true", "sh", exe)
	cmd.Env = append(os.Environ(), "GUARD_LINE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
		if err == nil && len(strings.Fields(string(out))) > 0 {
			if pid, err := strconv.Atoi(strings.Fields(string(out))[0]); err == nil && guardExecutableIsTest(pid) {
				return cmd, pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no test-binary child")
	return nil, 0
}

func newLineServer(t *testing.T) *Server {
	teststate.Lock(t)
	s := newTestServer(t, "guard-line")
	t.Cleanup(func() { s.sup.KillAll() })
	s.initDecisionHub()
	return s
}

func answeredDecision(t *testing.T, s *Server, option string) api.Decision {
	t.Helper()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	timeout := uint64(1)
	d, err := store.CreateDecision(api.DecisionCreateParams{Kind: "command_guard", Title: "実行確認", TimeoutMS: &timeout, Options: []api.DecisionOption{{ID: "approve", Label: "承認", Role: "approve"}, {ID: "reject", Label: "却下", Role: "reject"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	switch option {
	case "pending":
		return d
	case "expired":
		d, err = store.ExpireDecision(d.DecisionID, time.Now().Add(time.Hour))
	default:
		d, err = store.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, Responder: "test", OptionID: &option}, time.Now())
	}
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func bindOwner(t *testing.T, s *Server, d api.Decision, owner int, pane string, claims ...string) {
	t.Helper()
	_, identity, err := guard.ApprovalProcess(owner)
	if err != nil {
		t.Fatal(err)
	}
	var list []api.GuardExecutionClaim
	for _, c := range claims {
		list = append(list, api.GuardExecutionClaim{CommandHash: claimOfString(c), Rule: api.GuardLineApprovedRule})
	}
	if s.decisions.guardBindings == nil {
		s.decisions.guardBindings = map[string]guardExecutionBinding{}
	}
	s.decisions.guardBindings[d.DecisionID] = guardExecutionBinding{Owner: owner, ProcessStart: identity, Pane: pane, Claims: list}
}

func lineQuery(claim, pane string) api.GuardApprovedParams {
	return api.GuardApprovedParams{GuardExecutionClaim: api.GuardExecutionClaim{CommandHash: claimOfString(claim), Rule: api.GuardLineApprovedRule}, PaneID: pane}
}

// SPEC-line-approval.md 6章 (b): サーバーの単体
func TestLineApprovalServerMatching(t *testing.T) {
	s := newLineServer(t)
	owner, peer := startOwner(t)
	d := answeredDecision(t, s, "approve")
	bindOwner(t, s, d, owner.Process.Pid, "p1", "create-table")
	s.markGuardLineApproved(d, time.Now())

	if !s.guardExecutionApproved(peer, lineQuery("create-table", "p1")) {
		t.Fatal("a descendant of the owner with a listed claim was not approved")
	}
	// 使っても消さない（同じ claim を続けて聞いても真）。
	if !s.guardExecutionApproved(peer, lineQuery("create-table", "p1")) {
		t.Fatal("claim was consumed by use")
	}
	if s.guardExecutionApproved(peer, lineQuery("delete-table", "p1")) {
		t.Fatal("create-table's approval covered delete-table")
	}
	if s.guardExecutionApproved(os.Getpid(), lineQuery("create-table", "p1")) {
		t.Fatal("a process that is not a descendant of the owner was approved")
	}
	if s.guardExecutionApproved(peer, lineQuery("create-table", "p2")) {
		t.Fatal("another pane was approved")
	}
	// 規則名が違う claim は、行の claim と別物。
	wrong := lineQuery("create-table", "p1")
	wrong.Rule = "rm-paths"
	if s.guardExecutionApproved(peer, wrong) {
		t.Fatal("claim with another rule name matched")
	}
}

func TestLineApprovalNeedsAnApprovedDecision(t *testing.T) {
	s := newLineServer(t)
	owner, peer := startOwner(t)
	for _, status := range []string{"pending", "reject", "expired"} {
		d := answeredDecision(t, s, status)
		bindOwner(t, s, d, owner.Process.Pid, "p1", "c-"+status)
		s.markGuardLineApproved(d, time.Now())
		if s.guardExecutionApproved(peer, lineQuery("c-"+status, "p1")) {
			t.Fatalf("%s decision approved a claim", status)
		}
	}
}

func TestLineApprovalExpiresAfterTenMinutes(t *testing.T) {
	s := newLineServer(t)
	owner, peer := startOwner(t)
	d := answeredDecision(t, s, "approve")
	bindOwner(t, s, d, owner.Process.Pid, "p1", "c")
	s.markGuardLineApproved(d, time.Now().Add(-9*time.Minute))
	if !s.guardExecutionApproved(peer, lineQuery("c", "p1")) {
		t.Fatal("approval inside 10 minutes was refused")
	}
	b := s.decisions.guardBindings[d.DecisionID]
	b.ApprovedAt = time.Now().Add(-11 * time.Minute)
	s.decisions.guardBindings[d.DecisionID] = b
	if s.guardExecutionApproved(peer, lineQuery("c", "p1")) {
		t.Fatal("approval older than 10 minutes was accepted")
	}
	// 承認されていない束（ApprovedAt が無い）は、判断待ちが承認済みでも通さない。
	d2 := answeredDecision(t, s, "approve")
	bindOwner(t, s, d2, owner.Process.Pid, "p1", "never-marked")
	if s.guardExecutionApproved(peer, lineQuery("never-marked", "p1")) {
		t.Fatal("binding without ApprovedAt was accepted")
	}
}

func TestLineApprovalEndsWithOwner(t *testing.T) {
	s := newLineServer(t)
	owner, peer := startOwner(t)
	d := answeredDecision(t, s, "approve")
	bindOwner(t, s, d, owner.Process.Pid, "p1", "c")
	s.markGuardLineApproved(d, time.Now())
	if !s.guardExecutionApproved(peer, lineQuery("c", "p1")) {
		t.Fatal("precondition")
	}
	_ = owner.Process.Kill()
	_ = owner.Wait()
	if s.guardExecutionApproved(peer, lineQuery("c", "p1")) || len(s.decisions.guardBindings) != 0 {
		t.Fatal("approval outlived its owner")
	}
}

// 対話の zsh で新しい行が来たら、そのシェルが持ち主の束だけ消える。
func TestLineStartClearsOnlyThatShellsBindings(t *testing.T) {
	s := newLineServer(t)
	shell, peer := startOwner(t) // peer は shell の子（次の行の guard line）
	other, _ := startOwner(t)
	d1, d2 := answeredDecision(t, s, "approve"), answeredDecision(t, s, "approve")
	bindOwner(t, s, d1, shell.Process.Pid, "p1", "a")
	bindOwner(t, s, d2, other.Process.Pid, "p1", "b")
	_, start, err := guard.ApprovalProcess(peer)
	if err != nil {
		t.Fatal(err)
	}
	req := func(lineStart bool) *api.Request {
		return &api.Request{ID: "x", PeerPID: peer, Params: api.MarshalParams(api.GuardIdentityParams{PaneID: "p1", PID: peer, StartTime: start, LineStart: lineStart})}
	}
	s.guardIdentity(req(false))
	if len(s.decisions.guardBindings) != 2 {
		t.Fatal("identity without LineStart cleared bindings")
	}
	s.guardIdentity(req(true))
	if _, ok := s.decisions.guardBindings[d1.DecisionID]; ok {
		t.Fatal("the shell's own binding survived the next line")
	}
	if _, ok := s.decisions.guardBindings[d2.DecisionID]; !ok {
		t.Fatal("another shell's binding was cleared")
	}
}

func TestGuardLineViaAndArgv(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		via  string
		ok   bool
	}{
		{[]string{"ikesu", "guard", "line", "--via", "zsh-debug", "--", "x"}, "zsh-debug", true},
		{[]string{"ikesu", "guard", "line", "--via", "bash-debug", "--shell-pid", "1", "--", "x"}, "bash-debug", true},
		{[]string{"ikesu", "guard", "line", "--via", "accept-line", "--", "x"}, "accept-line", true},
		{[]string{"ikesu", "guard", "line", "--via", "exec-string", "--", "x"}, "exec-string", true},
		{[]string{"ikesu", "guard", "line", "--", "x"}, "", false},
		{[]string{"ikesu", "guard", "line", "--via", "shim", "--", "x"}, "shim", false},
		{[]string{"ikesu", "guard", "line", "--", "--via", "zsh-debug"}, "", false},
		{[]string{"ikesu", "guard", "exec", "--via", "zsh-debug", "--", "x"}, "", false},
		{[]string{"ikesu", "guard", "claude-prefix", "--", "x"}, "", false},
	} {
		if via, ok := guardLineViaOf(tc.argv); via != tc.via || ok != tc.ok {
			t.Errorf("%v: via=%q ok=%v", tc.argv, via, ok)
		}
	}
	got := guardArgv([]string{"ikesu", "--session", "e2e", "guard", "line", "--", "x"})
	if strings.Join(got, " ") != "ikesu guard line -- x" {
		t.Errorf("guardArgv: %v", got)
	}
	if strings.Join(guardArgv([]string{"ikesu", "--session=e2e", "guard", "exec"}), " ") != "ikesu guard exec" {
		t.Error("guardArgv with --session=")
	}
}

// guard line --via の判断待ちは持ち主が guard の親、--via の無い line と claude-prefix は peer のまま。
func TestLineApprovalOwnerFromVia(t *testing.T) {
	s := newLineServer(t)
	shell, peer := startOwnerWithTestPeer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := guardProcArgs
	defer func() { guardProcArgs = saved }()
	for _, tc := range []struct {
		argv      []string
		wantOwner int
	}{
		{[]string{exe, "guard", "line", "--via", "zsh-debug", "--", "x"}, shell.Process.Pid},
		{[]string{exe, "guard", "line", "--", "x"}, peer},
		{[]string{exe, "guard", "claude-prefix", "--", "x"}, peer},
		{[]string{exe, "--session", "s", "guard", "claude-prefix", "--", "x"}, peer},
	} {
		guardProcArgs = func(int) []string { return tc.argv }
		d := answeredDecision(t, s, "pending")
		params := api.DecisionCreateParams{Kind: "command_guard", Origin: &api.DecisionOrigin{PaneID: "p1"}, GuardExecutions: []api.GuardExecutionClaim{{CommandHash: claimOfString("a"), Rule: api.GuardLineApprovedRule}}}
		s.bindGuardExecution(peer, params, d)
		b, ok := s.decisions.guardBindings[d.DecisionID]
		if !ok || b.Owner != tc.wantOwner {
			t.Errorf("%v: binding=%+v ok=%v want owner %d", tc.argv, b, ok, tc.wantOwner)
		}
	}
}

// 実行ファイルが試験のバイナリでない peer は束を作れない（素の decision.create は束を持てない）。
func guardExecutableIsTest(peer int) bool {
	path, err := guard.ApprovalExecutable(peer)
	if err != nil {
		return false
	}
	own, err := os.Executable()
	if err != nil {
		return false
	}
	a, e1 := os.Stat(path)
	b, e2 := os.Stat(own)
	return e1 == nil && e2 == nil && os.SameFile(a, b)
}
