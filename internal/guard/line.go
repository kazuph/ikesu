package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// LineInput exposes the first parsed command for existing callers.
func LineInput(i Input, line string) (Input, error) {
	commands, err := lineCommands(i, line)
	if len(commands) > 0 {
		return commands[0], err
	}
	i.Command, i.Args, i.RawLine = "", nil, line
	return i, err
}
func AuthorizeLine(c Config, root string, i Input, line string, errOut io.Writer, await Await) bool {
	errOut = redactedWriter{errOut, i}
	m, active, e := ReadMarker(i)
	if e != nil {
		return authorize(c, root, i, "line", errOut, await, nil)
	}
	if !active {
		return true
	}
	c = *m.Policy
	if !c.Active() {
		return true
	}
	i.LaunchCwd = m.Cwd
	callerEnv := i.Env
	snapshot := i.shellVariables
	arrays := i.shellArrays
	runtimeLine := i.runtimeLine
	if trustedDirenvLine(i, line) {
		return true
	}
	i.AuthorizationEnv = EnvMap(environment(i.Env))
	i, verdict, parseErr := lineVerdict(c, i, line)
	i.shellVariables = snapshot
	i.shellArrays = arrays
	i.runtimeLine = runtimeLine
	// Keep the caller's authorization context across env -i/-u, while retaining
	// inline request reasons and approval-file settings from the parsed command.
	parsedEnv := i.Env
	authorizationEnv := i.AuthorizationEnv
	i.Env = EnvMap(environment(callerEnv))
	for key, value := range authorizationEnv {
		if !authorizationIdentityKey(key) {
			i.Env[key] = value
		}
	}
	for key, value := range parsedEnv {
		if i.shellVariables != nil {
			continue
		}
		if !authorizationIdentityKey(key) {
			i.Env[key] = value
		}
	}
	if runtimeLine {
		// Runtime assignments belong to the script, not the original request.
		for _, key := range []string{requestReasonEnvName(Input{Env: callerEnv}), "AI_GUARD_REQUEST_REASON", "AI_GUARD_REQUEST_ENV_NAME", "IKESU_GUARD_PARENT_REASON"} {
			if value, exists := callerEnv[key]; exists {
				i.Env[key] = value
			} else {
				delete(i.Env, key)
			}
		}
	}
	if i.shellVariables != nil {
		i.TriggerCommand = line
		verdict.Reason = i.shellDiagnostic(verdict.Reason, line)
	}
	if parseErr != nil {
		fmt.Fprintln(errOut, i.shellDiagnostic(parseErr.Error(), line))
	}
	if verdict.Decision == "allow" {
		if strings.HasPrefix(verdict.Reason, undeterminedPrefix) {
			// 未確定を理由に通したことを、後から区別できるよう記録する。
			logUndetermined(root, i, m, verdict, line, errOut)
		}
		return true
	}
	return authorize(c, root, i, "line", errOut, await, &verdict)
}

// Inline shell assignments cannot redirect the caller's authorization context.
func authorizationIdentityKey(key string) bool {
	switch key {
	case "IKESU_GUARD_MARKER", "IKESU_PANE_ID", "IKESU_SOCKET_PATH":
		return true
	}
	return false
}

const undeterminedLineLimit, undeterminedReasonLimit = 512, 256
const undeterminedDedupWindow = 60 * time.Second

func truncateRunes(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}

// 「未確定を通した」記録は短く切り、同じ pane・行・理由は60秒に1回だけ残す。
func logUndetermined(root string, i Input, m Marker, v Verdict, line string, errOut io.Writer) {
	v.Reason = truncateRunes(v.Reason, undeterminedReasonLimit)
	record := Input{Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Env: i.Env, RawLine: truncateRunes(line, undeterminedLineLimit)}
	sum := sha256.Sum256([]byte(m.Pane + "\x00" + line + "\x00" + v.Reason))
	key := hex.EncodeToString(sum[:])
	if undeterminedSeenRecently(root, key) {
		return
	}
	if e := Log(root, record, m, v, "line"); e != nil {
		fmt.Fprintln(errOut, "guard log:", e)
	}
}

// L1 で通した記録は、実行時の見張りが同じ行を何度も判定するので、同じ pane・行・規則を60秒に1回だけ残す。
func l1SeenRecently(root string, m Marker, i Input, v Verdict) bool {
	sum := sha256.Sum256([]byte("l1\x00" + m.Pane + "\x00" + i.Line() + "\x00" + v.Rule))
	return undeterminedSeenRecently(root, hex.EncodeToString(sum[:]))
}

func undeterminedSeenRecently(root, key string) bool {
	d := filepath.Join(root, "guard")
	if os.MkdirAll(d, 0700) != nil {
		return false
	}
	f, e := os.OpenFile(filepath.Join(d, "undetermined-seen.json"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return false
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX) != nil {
		return false
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	seen := map[string]int64{}
	if data, e := io.ReadAll(f); e == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &seen)
	}
	now := time.Now()
	for k, t := range seen {
		if now.Sub(time.Unix(t, 0)) >= undeterminedDedupWindow {
			delete(seen, k)
		}
	}
	_, dup := seen[key]
	if !dup {
		seen[key] = now.Unix()
	}
	if data, e := json.Marshal(seen); e == nil {
		_ = f.Truncate(0)
		_, _ = f.WriteAt(data, 0)
	}
	return dup
}
