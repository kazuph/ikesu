package monitor

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var (
	errOwner      = errors.New("プロセスが終了したか、起動識別子・ペインの所属が変更されました")
	errExecutable = errors.New("対象の実行ファイルが変更されたか、照合できません")
	errBinding    = errors.New("対象をペインのセッションまたはプロセスグループに照合できません")
)

// Target describes the specific executable the user saw in the alert.
type Target struct {
	Identity
	Path string
}

func Executable(pid int) (string, error) {
	path, err := executablePath(pid)
	if err != nil || !filepath.IsAbs(path) {
		return "", errExecutable
	}
	return path, nil
}

func validateOwner(target, root Identity, roots Roots, read readProcess) error {
	if target.PID <= 1 || root.PID <= 1 || target.Start == "" || root.Start == "" {
		return errOwner
	}
	others := make(map[int]bool)
	for _, candidate := range roots {
		if candidate != root {
			others[candidate.PID] = true
		}
	}
	seen := make(map[int]bool)
	var first Process
	for pid := target.PID; pid > 1 && !seen[pid]; {
		if others[pid] {
			return errOwner
		}
		seen[pid] = true
		current, err := read(pid)
		if err != nil || current.PID != pid || pid == target.PID && current.Identity != target {
			return errOwner
		}
		if pid == target.PID {
			first = current
		}
		if pid == root.PID {
			if current.Identity != root {
				return errOwner
			}
			if pid != target.PID {
				again, err := read(target.PID)
				if err != nil || again.Identity != target || again.Parent != first.Parent {
					return errOwner
				}
			}
			return nil
		}
		pid = current.Parent
	}
	return errOwner
}

func CheckTarget(target Target) error { return checkTarget(target, processInfo, Executable) }

func checkTarget(target Target, read readProcess, executable func(int) (string, error)) error {
	path, err := executable(target.PID)
	if err != nil || target.Path == "" || path != target.Path {
		return errExecutable
	}
	return checkIdentity(target.Identity, read)
}

func CheckIdentity(identity Identity) error { return checkIdentity(identity, processInfo) }

func checkIdentity(identity Identity, read readProcess) error {
	current, err := read(identity.PID)
	if err != nil || current.Identity != identity {
		return errOwner
	}
	return nil
}

// signalBound checks a Darwin pane session and revalidates after reading
// the executable. Signals always address a single positive PID.
func signalBound(target Target, root Identity, roots Roots, sig unix.Signal, read readProcess,
	executable func(int) (string, error), session func(int) (int, error), send func(int, unix.Signal) error) error {
	if err := validateOwner(target.Identity, root, roots, read); err != nil {
		return err
	}
	original, err := read(target.PID)
	if err != nil || original.Identity != target.Identity || original.Group <= 1 {
		return errBinding
	}
	rootSID, rootErr := session(root.PID)
	sid, sidErr := session(target.PID)
	if rootErr != nil || rootSID <= 1 || sidErr == nil && sid != rootSID {
		return errBinding
	}
	if sidErr != nil {
		leader, err := read(original.Group)
		if err != nil || leader.PID != original.Group || leader.Group != original.Group ||
			validateOwner(leader.Identity, root, roots, read) != nil {
			return errBinding
		}
		if leaderSID, err := session(leader.PID); err != nil || leaderSID != rootSID {
			return errBinding
		}
	}
	if err := validateOwner(target.Identity, root, roots, read); err != nil {
		return err
	}
	path, err := executable(target.PID)
	if err != nil || target.Path == "" || path != target.Path {
		return errExecutable
	}
	latest, err := read(target.PID)
	if err != nil || latest.Identity != target.Identity || latest.Parent != original.Parent || latest.Group != original.Group {
		return errBinding
	}
	// Darwin has no instance-bound signal syscall. The final check and
	// kill are adjacent but the kernel does not make them atomic.
	return send(target.PID, sig)
}

func Signal(target Target, root Identity, roots Roots, force bool) error {
	if target.PID <= 1 || target.Start == "" || target.Path == "" {
		return errOwner
	}
	sig := unix.SIGTERM
	if force {
		sig = unix.SIGKILL
	}
	return signalProcess(target, root, roots, sig)
}
