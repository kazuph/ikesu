package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func invocationHash(i Input) (string, error) {
	executable, err := Resolve(i.Command, environment(i.Env))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Cwd, Executable string
		Argv            []string
		Identity        executableIdentity
	}{physical(i.Cwd), physical(executable), append([]string{i.Command}, i.Args...), executableFileIdentity(info)})
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
	if !immutableInvocation(*i.scriptResult.approvedCommand) {
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
	if !immutableInvocation(i) {
		return false
	}
	digest, err := invocationHash(i)
	return err == nil && i.ApprovedInvocation(digest, rule)
}

// The server uses kernel identities, never a caller-provided PID or marker.
func ApprovalProcess(pid int) (int, string, error) { return approvalProcess(pid) }
func ApprovalExecutable(pid int) (string, error)   { return approvalExecutable(pid) }

type executableIdentity struct {
	Device, Inode                                            uint64
	Size, ModifiedSec, ModifiedNSec, ChangedSec, ChangedNSec int64
}

func immutableInvocation(i Input) bool {
	path, err := Resolve(i.Command, environment(i.Env))
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	// Check both the lookup path (including symlink containers) and resolved target.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	for _, current := range []string{path, resolved} {
		for {
			if err := unix.Access(current, unix.W_OK); err != unix.EACCES && err != unix.EPERM && err != unix.EROFS {
				return false
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return true
}
