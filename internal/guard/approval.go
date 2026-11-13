package guard

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func regularFile(p string) bool { st, e := os.Stat(p); return e == nil && st.Mode().IsRegular() }
func validApproval(f, k string, now time.Time) bool {
	if k == "" {
		return false
	}
	b, e := os.ReadFile(f)
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		v := strings.Split(line, "\t")
		if len(v) != 2 {
			continue
		}
		expiry, e := strconv.ParseInt(v[1], 10, 64)
		if e == nil && approvalKeyMatches(v[0], k) && expiry > now.Unix() {
			return true
		}
	}
	return false
}

// Day grants follow the physical directory tree, never a textual prefix or
// repository membership. Other approval keys retain exact matching.
func approvalKeyMatches(saved, requested string) bool {
	if !strings.HasPrefix(requested, "day:") {
		return saved == requested
	}
	if !strings.HasPrefix(saved, "day:") {
		return false
	}
	root, err := filepath.EvalSymlinks(strings.TrimPrefix(saved, "day:"))
	if err != nil || !filepath.IsAbs(root) {
		return false
	}
	cwd, err := filepath.EvalSymlinks(strings.TrimPrefix(requested, "day:"))
	if err != nil || !filepath.IsAbs(cwd) {
		return false
	}
	relative, err := filepath.Rel(root, cwd)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func WriteApproval(f, k string, expiry time.Time) error {
	if k == "" {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(f), 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(f+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX); e != nil {
		return e
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	lines := []string{}
	if old, e := os.Open(f); e == nil {
		s := bufio.NewScanner(old)
		for s.Scan() {
			v := strings.Split(s.Text(), "\t")
			if len(v) == 2 {
				n, e := strconv.ParseInt(v[1], 10, 64)
				if e == nil && n > time.Now().Unix() && v[0] != k {
					lines = append(lines, s.Text())
				}
			}
		}
		e = s.Err()
		old.Close()
		if e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	lines = append(lines, fmt.Sprintf("%s\t%d", k, expiry.Unix()))
	tmp, e := os.CreateTemp(filepath.Dir(f), ".guard-approval-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.WriteString(strings.Join(lines, "\n") + "\n")
	}
	ce := tmp.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp.Name(), f)
}
func environment(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
func Resolve(cmd string, env []string) (string, error) {
	m := EnvMap(env)
	if strings.Contains(cmd, "/") {
		if childenv.IsGuardShimDirectory(filepath.Dir(cmd), env) || isForwardingShim(cmd) {
			return "", fmt.Errorf("refusing to execute guard shim: %s", cmd)
		}
		if executable(cmd) {
			return cmd, nil
		}
		return "", fmt.Errorf("command is not executable: %s", cmd)
	}
	rejectedShim := false
	for _, d := range strings.Split(m["PATH"], string(os.PathListSeparator)) {
		if m["IKESU_GUARD_BIN"] != "" && physical(d) == physical(m["IKESU_GUARD_BIN"]) {
			continue
		}
		if d == "" {
			d = "."
		}
		if childenv.IsGuardShimDirectory(d, env) {
			continue
		}
		p := filepath.Join(d, cmd)
		if executable(p) {
			if isForwardingShim(p) {
				rejectedShim = true
				continue
			}
			return p, nil
		}
	}
	if rejectedShim {
		return "", fmt.Errorf("refusing to execute guard shim: no real command found for %s", cmd)
	}
	return "", fmt.Errorf("command not found: %s", cmd)
}
func executable(p string) bool {
	st, e := os.Stat(p)
	return e == nil && !st.IsDir() && st.Mode()&0111 != 0
}
func EnvMap(env []string) map[string]string {
	m := map[string]string{}
	for _, v := range env {
		k, s, ok := strings.Cut(v, "=")

		if ok {
			m[k] = s
		}
	}
	return m
}

// 一時許可・1日許可の置き場所と長さ。コマンドの環境変数からは取らない。置き場所は guard の
// 状態のフォルダの下で、AI が書こうとすると guard-boundary の確認（L3）になる。
const tempApprovalDuration = 180 * time.Second

func tempApprovalFile(root string) string { return filepath.Join(root, "guard", "temp-approvals") }

// 使い切りの許可（L3）。L3 の確認を人が承認したとき、同じコマンドが guard の別の段
// （シム・bash の DEBUG・zsh の accept-line）でもう一度判定される分を、承認から30秒の間に
// 1回だけ通す。180秒の一時許可とは別のファイルで、L3 には一時許可を効かせない決まりは変えない。
const oneShotDuration = 30 * time.Second

// parentPID は guard 自身のプロセスの親（判定を呼んだシェル）。AI が変えられる環境変数からは取らない。
var parentPID = os.Getppid

func oneShotFile(root string) string { return filepath.Join(root, "guard", "one-shot-approvals") }

// OneShotTarget は L3 になったコマンド1つ。Cmd は、どの段から見ても同じになる形（コマンド名の末尾＋引数）。
type OneShotTarget struct{ Cwd, Cmd string }

func oneShotCommand(command string, args []string) string {
	return strings.Join(append([]string{filepath.Base(command)}, args...), "\x00")
}
func oneShotTargetOf(i Input) OneShotTarget {
	return OneShotTarget{Cwd: physical(i.Cwd), Cmd: oneShotCommand(i.Command, i.Args)}
}

// oneShotKey のキーは、ペイン（本人確認で確かめた目印の値）・シェルの PID（guard の親）・
// cwd の物理パス・コマンドの文字列。
func oneShotKey(m Marker, t OneShotTarget) string {
	data, _ := json.Marshal(struct {
		Pane    string
		Outside bool
		Shell   int
		Cwd     string
		Cmd     string
	}{m.Pane, m.OutsidePane, parentPID(), t.Cwd, t.Cmd})
	sum := sha256.Sum256(data)
	return "one-shot:" + hex.EncodeToString(sum[:])
}

func writeOneShot(root string, m Marker, targets []OneShotTarget, now time.Time) error {
	for _, t := range targets {
		if err := WriteApproval(oneShotFile(root), oneShotKey(m, t), now.Add(oneShotDuration)); err != nil {
			return err
		}
	}
	return nil
}

// takeOneShot は、全部のコマンドの許可が残っていれば、それらを消して true を返す（読みと消しは
// 同じロックの中）。1つでも欠けていれば何も消さない。期限切れは照合しない。
func takeOneShot(root string, m Marker, targets []OneShotTarget, now time.Time) bool {
	if len(targets) == 0 {
		return false
	}
	f := oneShotFile(root)
	lock, e := os.OpenFile(f+".lock", os.O_RDWR, 0600)
	if e != nil {
		return false
	}
	defer lock.Close()
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX) != nil {
		return false
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	data, e := os.ReadFile(f)
	if e != nil {
		return false
	}
	live := map[string]string{}
	var order []string
	for _, line := range strings.Split(string(data), "\n") {
		v := strings.Split(line, "\t")
		if len(v) != 2 {
			continue
		}
		if expiry, e := strconv.ParseInt(v[1], 10, 64); e == nil && expiry > now.Unix() {
			if _, dup := live[v[0]]; !dup {
				order = append(order, v[0])
			}
			live[v[0]] = line
		}
	}
	for _, t := range targets {
		if _, ok := live[oneShotKey(m, t)]; !ok {
			return false
		}
	}
	for _, t := range targets {
		delete(live, oneShotKey(m, t))
	}
	var lines []string
	for _, k := range order {
		if l, ok := live[k]; ok {
			lines = append(lines, l)
		}
	}
	tmp, e := os.CreateTemp(filepath.Dir(f), ".guard-approval-")
	if e != nil {
		return false
	}
	defer os.Remove(tmp.Name())
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.WriteString(body)
	}
	ce := tmp.Close()
	if e != nil || ce != nil || os.Rename(tmp.Name(), f) != nil {
		return false // 消せなかったときは通さない
	}
	return true
}

// applyGrants は、承認済みの一時許可・1日許可が残っている ask を allow にする。
func applyGrants(root string, v Verdict, now time.Time) Verdict {
	if v.Decision != "ask" || v.TempKey == "" {
		return v
	}
	f := tempApprovalFile(root)
	if validApproval(f, v.DayKey, now) {
		v.Decision, v.Suppressed, v.AllowedBy = "allow", "day", "day"
	} else if validApproval(f, v.TempKey, now) {
		v.Decision, v.Suppressed, v.AllowedBy = "allow", "temporary", "temporary"
	}
	return v
}

// RecordDayApproval stores a "1日許可" for cwd until the next local
// midnight. The server calls it when the answer arrives, so the approval
// holds even when the command that asked was gone by then (2026-11-09: test
// processes that asked were killed before they could store it, and the same
// question kept coming back). root is the guard state root (api.StateDir()).
func RecordDayApproval(root, cwd string) error {
	if cwd == "" || root == "" {
		return nil
	}
	now := time.Now()
	return WriteApproval(tempApprovalFile(root), "day:"+physical(cwd), time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location()))
}
