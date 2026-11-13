package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
)

type processIdentity struct {
	pid, parent int
	start       string
}

func guardStartBefore(parent, child string) bool {
	parse := func(value string) ([]uint64, bool) {
		parts := strings.Split(value, ":")
		if len(parts) < 1 || len(parts) > 2 {
			return nil, false
		}
		result := make([]uint64, len(parts))
		for n, part := range parts {
			v, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return nil, false
			}
			result[n] = v
		}
		return result, true
	}
	a, ok := parse(parent)
	if !ok {
		return false
	}
	b, ok := parse(child)
	if !ok || len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n] != b[n] {
			return a[n] < b[n]
		}
	}
	return true
}

// guardAncestry checks each generation twice, including parent links. An
// unreadable or changing process is indeterminate, never an outside result.
func guardAncestry(peer int) ([]processIdentity, error) {
	seen := map[int]bool{}
	var chain []processIdentity
	for pid := peer; pid > 1; {
		if seen[pid] {
			return nil, fmt.Errorf("process ancestry is cyclic or too deep")
		}
		seen[pid] = true
		parent, start, err := guard.ApprovalProcess(pid)
		if err != nil || parent < 1 || start == "" {
			return nil, fmt.Errorf("cannot inspect process ancestry")
		}
		chain = append(chain, processIdentity{pid, parent, start})
		if len(chain) > 1 && !guardStartBefore(start, chain[len(chain)-2].start) {
			return nil, fmt.Errorf("process ancestry generation order is inconsistent")
		}
		pid = parent
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("missing peer identity")
	}
	for _, p := range chain {
		parent, start, err := guard.ApprovalProcess(p.pid)
		if err != nil || parent != p.parent || start != p.start {
			return nil, fmt.Errorf("process identity changed during verification")
		}
	}
	return chain, nil
}

func (s *Server) guardIdentity(req *api.Request) api.Response {
	var p api.GuardIdentityParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if p.PID <= 1 || p.PID != req.PeerPID || p.StartTime == "" {
		return api.Failure(req.ID, "guard_identity_unknown", "guard process identity does not match peer")
	}
	_, start, err := guard.ApprovalProcess(p.PID)
	if err != nil || start != p.StartTime {
		return api.Failure(req.ID, "guard_identity_unknown", "guard process generation is unavailable or changed")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(p.PaneID, "p"))
	if err != nil || n <= 0 || p.PaneID != "p"+strconv.Itoa(n) {
		return api.Failure(req.ID, api.ErrInvalidRequest, "invalid guard pane")
	}
	if p.LineStart {
		// 対話のシェルで新しい行が来た。その行より前に、このシェルが持ち主だった行の承認を消す。
		if parent, _, err := guard.ApprovalProcess(p.PID); err == nil && parent > 1 {
			s.clearGuardLineBindings(parent)
		}
	}
	chain, err := guardAncestry(req.PeerPID)
	if err != nil {
		return api.Failure(req.ID, "guard_identity_unknown", err.Error())
	}
	match := false
	if _, _, _, exists := s.findPane(p.PaneID); exists {
		pane := s.sup.Get(n)
		if pane == nil {
			return api.Failure(req.ID, "guard_identity_unknown", "pane process unavailable")
		}
		pid, start := pane.ShellIdentity()
		if pid <= 1 || start == "" {
			return api.Failure(req.ID, "guard_identity_unknown", "pane generation unavailable")
		}
		for _, process := range chain {
			if process.pid == pid {
				if process.start != start {
					return api.Failure(req.ID, "guard_identity_unknown", "pane generation changed")
				}
				match = true
				break
			}
		}
	}
	if match {
		marker := filepath.Join(api.StateDir(), "guard", "agent-panes", "p_"+strconv.Itoa(n)+".json")
		if _, err := os.Lstat(marker); os.IsNotExist(err) {
			match = false
		} else if err != nil {
			return api.Failure(req.ID, "guard_identity_unknown", "cannot inspect pane marker")
		}
	}
	policy := api.MarshalParams(s.cfg.Guard)
	digest := sha256.Sum256(policy)
	return api.Success(req.ID, api.GuardIdentityResult{Type: "guard_identity", Match: match, Policy: policy, PolicyDigest: hex.EncodeToString(digest[:])})
}
