package server

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/platform"
	"os"
)

// Only the app loop reads/writes this state. It is not loaded from shell
// variables or guard files that the script could replace.
type guardExecutionBinding struct {
	Owner        int
	ProcessStart string
	Pane         string
	Claim        api.GuardExecutionClaim
}

func (s *Server) bindGuardExecution(peer int, params api.DecisionCreateParams, decision api.Decision) {
	if peer <= 1 || params.Kind != "command_guard" || params.Origin == nil || params.GuardExecution == nil {
		return
	}
	decoded, err := hex.DecodeString(params.GuardExecution.CommandHash)
	if err != nil || len(decoded) != sha256.Size {
		return
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
	argv := platform.ProcArgs(peer)
	if len(argv) < 3 || argv[1] != "guard" || (argv[2] != "claude-prefix" && argv[2] != "line" && argv[2] != "exec") {
		return
	}
	_, identity, err := guard.ApprovalProcess(peer)
	if err != nil {
		return
	}
	if s.decisions.guardBindings == nil {
		s.decisions.guardBindings = map[string]guardExecutionBinding{}
	}
	s.decisions.guardBindings[decision.DecisionID] = guardExecutionBinding{Owner: peer, ProcessStart: identity, Pane: params.Origin.PaneID, Claim: *params.GuardExecution}
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
		if ancestors[binding.Owner] != binding.ProcessStart || params.PaneID != binding.Pane || params.GuardExecutionClaim != binding.Claim {
			continue
		}
		decision, err := store.Decision(id)
		if err == nil && decision.Status == "answered" && decision.Answer != nil && decision.Answer.OptionID != nil && (*decision.Answer.OptionID == "approve" || *decision.Answer.OptionID == "day_allow") {
			return true
		}
	}
	return false
}

func (s *Server) pruneGuardExecutions() {
	for id, binding := range s.decisions.guardBindings {
		_, identity, err := guard.ApprovalProcess(binding.Owner)
		if err != nil || identity != binding.ProcessStart {
			delete(s.decisions.guardBindings, id)
		}
	}
}
