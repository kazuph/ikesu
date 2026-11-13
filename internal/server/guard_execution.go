package server

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/platform"
	"os"
	"strings"
	"time"
)

// Only the app loop reads/writes this state. It is not loaded from shell
// variables or guard files that the script could replace.
type guardExecutionBinding struct {
	Owner        int
	ProcessStart string
	Pane         string
	Claims       []api.GuardExecutionClaim
	// 行の承認（規則名 line-approved）の claim だけが使う。承認された時刻。
	ApprovedAt time.Time
}

// 行の承認の claim が有効な長さ。
const guardLineApprovalTTL = 10 * time.Minute

// 行の判定を呼ぶ guard line の --via の種類。1段目の確認を出す側で、持ち主は guard の親（シェル）。
var guardLineVia = map[string]bool{"accept-line": true, "exec-string": true, "zsh-debug": true, "bash-debug": true}

func guardLineViaOf(argv []string) (string, bool) {
	if len(argv) < 3 || argv[1] != "guard" || argv[2] != "line" {
		return "", false
	}
	for n := 3; n+1 < len(argv); n++ {
		if argv[n] == "--" {
			break
		}
		if argv[n] == "--via" {
			return argv[n+1], guardLineVia[argv[n+1]]
		}
	}
	return "", false
}

// guardArgv は、先頭の --session <名前> を除いた argv（[実行ファイル, guard, 種類, ...]）。
func guardArgv(argv []string) []string {
	if len(argv) >= 3 && argv[1] == "--session" {
		return append([]string{argv[0]}, argv[3:]...)
	}
	if len(argv) >= 2 && strings.HasPrefix(argv[1], "--session=") {
		return append([]string{argv[0]}, argv[2:]...)
	}
	return argv
}

func validClaimHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size
}

func (s *Server) bindGuardExecution(peer int, params api.DecisionCreateParams, decision api.Decision) {
	if peer <= 1 || params.Kind != "command_guard" || params.Origin == nil || params.GuardExecution == nil && len(params.GuardExecutions) == 0 {
		return
	}
	var claims []api.GuardExecutionClaim
	if params.GuardExecution != nil {
		if !validClaimHash(params.GuardExecution.CommandHash) {
			return
		}
		claims = append(claims, *params.GuardExecution)
	}
	for _, claim := range params.GuardExecutions {
		if len(claims) >= api.GuardLineApprovalLimit {
			break
		}
		if claim.Rule != api.GuardLineApprovedRule || !validClaimHash(claim.CommandHash) {
			return
		}
		claims = append(claims, claim)
	}
	// A raw decision.create request cannot supply a binding. The kernel peer
	// must be this actual CLI binary running a guard entry point.
	executable, err := guard.ApprovalExecutable(peer)
	if err != nil {
		return
	}
	actual, err := os.Stat(executable)
	if err != nil {
		return
	}
	ownPath, err := os.Executable()
	if err != nil {
		return
	}
	own, err := os.Stat(ownPath)
	if err != nil || !os.SameFile(actual, own) {
		return
	}
	argv := guardArgv(platform.ProcArgs(peer))
	if len(argv) < 3 || argv[1] != "guard" || (argv[2] != "claude-prefix" && argv[2] != "line" && argv[2] != "exec") {
		return
	}
	parent, identity, err := guard.ApprovalProcess(peer)
	if err != nil {
		return
	}
	owner := peer
	if _, ok := guardLineViaOf(argv); ok {
		// 行の判定は短い子プロセスなので、持ち主は親のシェルにする。
		parentParent, parentStart, err := guard.ApprovalProcess(parent)
		if err != nil || parent <= 1 || parentStart == "" || parentParent < 1 || !guardStartBefore(parentStart, identity) {
			return
		}
		again, _, err := guard.ApprovalProcess(peer)
		if err != nil || again != parent {
			return
		}
		owner, identity = parent, parentStart
	}
	if s.decisions.guardBindings == nil {
		s.decisions.guardBindings = map[string]guardExecutionBinding{}
	}
	s.decisions.guardBindings[decision.DecisionID] = guardExecutionBinding{Owner: owner, ProcessStart: identity, Pane: params.Origin.PaneID, Claims: claims}
}

func (s *Server) guardExecutionApproved(peer int, params api.GuardApprovedParams) bool {
	s.pruneGuardExecutions()
	ancestors := map[int]string{}
	seen := map[int]bool{}
	for pid := peer; pid > 1 && !seen[pid]; {
		seen[pid] = true
		parent, identity, err := guard.ApprovalProcess(pid)
		if err != nil {
			break
		}
		ancestors[pid] = identity
		pid = parent
	}
	store, err := s.db()
	if err != nil {
		return false
	}
	for id, binding := range s.decisions.guardBindings {
		if ancestors[binding.Owner] != binding.ProcessStart || params.PaneID != binding.Pane || !binding.hasClaim(params.GuardExecutionClaim) {
			continue
		}
		// 行の承認は、承認から10分で切れる。
		if params.Rule == api.GuardLineApprovedRule && (binding.ApprovedAt.IsZero() || time.Since(binding.ApprovedAt) > guardLineApprovalTTL) {
			continue
		}
		decision, err := store.Decision(id)
		if err == nil && decision.Status == "answered" && decision.Answer != nil && decision.Answer.OptionID != nil && (*decision.Answer.OptionID == "approve" || *decision.Answer.OptionID == "day_allow") {
			return true
		}
	}
	return false
}

func (b guardExecutionBinding) hasClaim(claim api.GuardExecutionClaim) bool {
	for _, c := range b.Claims {
		if c == claim {
			return true
		}
	}
	return false
}

// 承認された判断待ちの行の claim に、承認の時刻を入れる。
func (s *Server) markGuardLineApproved(d api.Decision, now time.Time) {
	binding, ok := s.decisions.guardBindings[d.DecisionID]
	if !ok || d.Status != "answered" || d.Answer == nil || d.Answer.OptionID == nil || (*d.Answer.OptionID != "approve" && *d.Answer.OptionID != "day_allow") {
		return
	}
	if binding.ApprovedAt.IsZero() {
		binding.ApprovedAt = now
		s.decisions.guardBindings[d.DecisionID] = binding
	}
}

// 対話のシェルで次の行が来たとき、そのシェルが持ち主の行の承認を消す。
func (s *Server) clearGuardLineBindings(owner int) {
	for id, binding := range s.decisions.guardBindings {
		if binding.Owner == owner {
			delete(s.decisions.guardBindings, id)
		}
	}
}

func (s *Server) pruneGuardExecutions() {
	for id, binding := range s.decisions.guardBindings {
		_, identity, err := guard.ApprovalProcess(binding.Owner)
		if err != nil || identity != binding.ProcessStart {
			delete(s.decisions.guardBindings, id)
		}
	}
}
