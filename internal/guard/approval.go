package guard

import (
	"bufio"
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/envcompat"
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
		if e == nil && v[0] == k && expiry > now.Unix() {
			return true
		}
	}
	return false
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
	return envcompat.Both(out)
}
func Resolve(cmd string, env []string) (string, error) {
	m := EnvMap(env)
	if strings.Contains(cmd, "/") {
		if childenv.IsGuardShimDirectory(filepath.Dir(cmd), env) {
			return "", fmt.Errorf("refusing to execute guard shim: %s", cmd)
		}
		if executable(cmd) {
			return cmd, nil
		}
		return "", fmt.Errorf("command is not executable: %s", cmd)
	}
	for _, d := range strings.Split(m["PATH"], string(os.PathListSeparator)) {
		if m["GTX_GUARD_BIN"] != "" && physical(d) == physical(m["GTX_GUARD_BIN"]) {
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
			return p, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", cmd)
}
func executable(p string) bool {
	st, e := os.Stat(p)
	return e == nil && !st.IsDir() && st.Mode()&0111 != 0
}
func EnvMap(env []string) map[string]string {
	m := map[string]string{}
	for _, v := range envcompat.Both(env) {
		k, s, ok := strings.Cut(v, "=")
		// Guard internals use one canonical key; export restores both names.
		if ok && !strings.HasPrefix(k, "IKESU_") {
			m[k] = s
		}
	}
	return m
}
