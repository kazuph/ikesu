package client

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/latency"
)

const upgradeLogFieldLimit = 512

var upgradeLogVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

const upgradeLogLimit = int64(latency.Limit)
const upgradeLogLimitMessage = "上限に達したので以後は記録しない\n"

func logUpgradeFailure(session, currentVersion string, target upgradeIdentity, reason error) {
	row := struct {
		Time           string `json:"time"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Executable     string `json:"executable"`
		Reason         string `json:"reason"`
	}{time.Now().Format(time.RFC3339Nano), upgradeLogVersion(currentVersion), upgradeLogVersion(target.Version), upgradeLogPath(target.Executable), upgradeFailureReason(reason)}
	line, err := json.Marshal(row)
	if err != nil {
		return
	}
	line = append(line, '\n')
	f, err := os.OpenFile(filepath.Join(api.SessionDir(session), "client-upgrade.log"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	info, err := f.Stat()
	if err != nil {
		return
	}
	size := info.Size()
	if size >= int64(len(upgradeLogLimitMessage)) {
		tail := make([]byte, len(upgradeLogLimitMessage))
		if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
			return
		}
		if string(tail) == upgradeLogLimitMessage {
			return
		}
	}
	if size+int64(len(line))+int64(len(upgradeLogLimitMessage)) > upgradeLogLimit {
		if size+int64(len(upgradeLogLimitMessage)) <= upgradeLogLimit {
			_, _ = f.WriteString(upgradeLogLimitMessage)
		}
		return
	}
	_, _ = f.Write(line)
}

// Error text can contain configuration values or remote server input.
func upgradeFailureReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "upgrade operation timed out"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "executable or connection file missing"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission denied"
	}
	text := err.Error()
	for _, entry := range []struct{ prefix, reason string }{
		{"client configuration:", "client configuration invalid"},
		{"version mismatch:", "version mismatch"},
		{"version check failed:", "version check failed"},
		{"current executable:", "current executable verification failed"},
		{"writable executable parent:", "writable executable parent"},
		{"send input before client upgrade:", "send pending input failed"},
		{"drain ", "drain endpoint input failed"},
		{"clipboard input is pending", "clipboard input pending"},
		{"connect to upgraded server:", "connect to upgraded server failed"},
		{"raw mode:", "terminal raw mode failed"},
	} {
		if strings.HasPrefix(text, entry.prefix) {
			return entry.reason
		}
	}
	for _, known := range []string{"version mismatch", "invalid ping response", "executable path is not absolute", "executable is not owned by the current user", "executable changed while opening", "file changed after verification", "server changed during client upgrade", "cannot send pending input before client upgrade", "input destination is changing during client upgrade", "local input is waiting for reconnection before client upgrade"} {
		if text == known {
			return known
		}
	}
	return "client upgrade failed; details omitted"
}

func upgradeLogVersion(value string) string {
	if len(value) > upgradeLogFieldLimit || !upgradeLogVersionPattern.MatchString(value) {
		return "invalid"
	}
	return value
}

func upgradeLogPath(value string) string {
	var result strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if result.Len()+utf8.RuneLen(r) > upgradeLogFieldLimit {
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}
