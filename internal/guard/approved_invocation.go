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

// lineClaimHash は、行の承認が通す claim。cwd と引数の並びだけのハッシュで、外側の承認のような
// 実行ファイルの置き場所の条件は付けない（Homebrew・mise の置き場所では偽になるため）。
// 引数は配列のまま入れ、空白でつながない。スクリプトを走らせる shell は、スクリプトのファイルの身元も入れる。
// コマンド名が空・未確定の語がある・cd の後で cwd が決まらないときは作らない。
func lineClaimHash(i Input) (string, bool) {
	if i.Command == "" || i.Unresolved || i.CwdUncertain {
		return "", false
	}
	var script *executableIdentity
	if has([]string{"sh", "bash", "zsh", "dash", "ksh"}, filepath.Base(i.Command)) {
		if path, isFile, _ := shellFileArgument(i.Args); isFile {
			if !filepath.IsAbs(path) {
				path = filepath.Join(i.Cwd, path)
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return "", false
			}
			identity := executableFileIdentity(info)
			script = &identity
		}
	}
	data, err := json.Marshal(struct {
		Kind   string
		Cwd    string
		Argv   []string
		Script *executableIdentity `json:",omitempty"`
	}{"line-v1", physical(i.Cwd), append([]string{i.Command}, i.Args...), script})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

// lineApproved は、1段目で承認された行の claim に、この判定が含まれるか。
// シム・関数（行の判定の控えが無い）は自分の claim 1つ。行は、ask の命令の全部に claim があり、
// 全部が承認済みのときだけ。行全体のエラー（shell-line）は通さない。
func lineApproved(i Input, v Verdict) bool {
	if i.ApprovedInvocation == nil {
		return false
	}
	if i.judgement == nil {
		hash, ok := lineClaimHash(i)
		return ok && i.ApprovedInvocation(hash, api.GuardLineApprovedRule)
	}
	if v.Rule == "shell-line" {
		return false
	}
	asks := 0
	for _, entry := range i.judgement.entries {
		if entry.v.Decision != "ask" {
			continue
		}
		asks++
		if entry.claim == "" || !i.ApprovedInvocation(entry.claim, api.GuardLineApprovedRule) {
			return false
		}
	}
	return asks > 0
}

// lineClaims は、判断待ちに付ける行の claim（重複なし・上限あり）。
func lineClaims(j *lineJudgement) []api.GuardExecutionClaim {
	if j == nil {
		return nil
	}
	seen := map[string]bool{}
	var claims []api.GuardExecutionClaim
	add := func(hash string) {
		if hash != "" && !seen[hash] && len(claims) < api.GuardLineApprovalLimit {
			seen[hash] = true
			claims = append(claims, api.GuardExecutionClaim{CommandHash: hash, Rule: api.GuardLineApprovedRule})
		}
	}
	for _, entry := range j.entries {
		if entry.v.Decision != "ask" {
			continue
		}
		add(entry.claim)
		for _, h := range entry.scriptClaims {
			add(h)
		}
	}
	return claims
}

// 行の claim を作る呼び出し。claude-prefix と、guard line の1段目（承認を出す側）の --via の種類だけ。
// guard exec（シム・関数）は最後の段なので送らない。
func lineClaimVia(via string) bool {
	return via == "claude-prefix" || has([]string{"accept-line", "exec-string", "zsh-debug", "bash-debug"}, via)
}
