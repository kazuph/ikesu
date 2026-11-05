package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/latency"
)

const upgradeLogLimit = int64(latency.Limit)
const upgradeLogLimitMessage = "上限に達したので以後は記録しない\n"

func logUpgradeFailure(session, currentVersion string, target upgradeIdentity, reason error) {
	row := struct {
		Time           string `json:"time"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Executable     string `json:"executable"`
		Reason         string `json:"reason"`
	}{time.Now().Format(time.RFC3339Nano), currentVersion, target.Version, target.Executable, reason.Error()}
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
