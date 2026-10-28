package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
)

func invocationHash(i Input) (string, error) {
	executable, err := Resolve(i.Command, environment(i.Env))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Cwd, Executable string
		Argv            []string
	}{physical(i.Cwd), physical(executable), append([]string{i.Command}, i.Args...)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func approvedInvocationClaim(i Input) *api.GuardExecutionClaim {
	if i.scriptResult == nil || i.scriptResult.approvedCommand == nil {
		return nil
	}
	digest, err := invocationHash(*i.scriptResult.approvedCommand)
	if err != nil {
		return nil
	}
	return &api.GuardExecutionClaim{CommandHash: digest, Rule: i.scriptResult.verdict.Rule}
}

func matchesApprovedInvocation(i Input, rule string) bool {
	if i.ApprovedInvocation == nil || i.Unresolved || i.CwdUncertain || rule == "guard-boundary" || rule == "shell-line" {
		return false
	}
	digest, err := invocationHash(i)
	return err == nil && i.ApprovedInvocation(digest, rule)
}

// The server uses kernel identities, never a caller-provided PID or marker.
func ApprovalProcess(pid int) (int, string, error) { return approvalProcess(pid) }
func ApprovalExecutable(pid int) (string, error)   { return approvalExecutable(pid) }
