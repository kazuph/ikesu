package session

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/platform"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Claude publishes a pid-bound runtime record before a conversation has its
// first project transcript. Only the live foreground pid can supply this id.
func observeClaudeRuntime(root, cwd string, start time.Time, job platform.ForegroundJob) (string, bool) {
	found := ""
	for _, process := range job.Processes {
		if process.PID <= 0 {
			continue
		}
		path := filepath.Join(root, "sessions", strconv.Itoa(process.PID)+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record struct {
			PID     int    `json:"pid"`
			ID      string `json:"sessionId"`
			Cwd     string `json:"cwd"`
			Started int64  `json:"startedAt"`
		}
		if json.Unmarshal(data, &record) != nil || record.PID != process.PID || record.Cwd != cwd || !SafeSessionID(record.ID) || record.Started <= 0 || absDur(time.UnixMilli(record.Started).Sub(start)) > sessionFileWindow {
			continue
		}
		if found != "" && found != record.ID {
			return "", false
		}
		found = record.ID
	}
	return found, found != ""
}
