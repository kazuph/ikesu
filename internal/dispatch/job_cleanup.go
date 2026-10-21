package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const JobRetention = 24 * time.Hour

// CleanupJobs holds a write transaction while unlinking replies and deleting
// expired records. SQLite's existing 5s busy timeout also applies here.
func (s *Store) CleanupJobs(now time.Time, logDir string, alive func(int) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			s.db.Exec("ROLLBACK")
		}
	}()
	st, err := s.queryLocked(`SELECT id, status, runner_pid, created_at, finished_at, log_path FROM dispatches WHERE kind='command'`)
	if err != nil {
		return err
	}
	type expiredJob struct {
		id  int64
		log string
	}
	var expired []expiredJob
	keptLogs := map[string]bool{}
	cutoff := now.Add(-JobRetention)
	for st.Step() {
		status := st.ColumnText(1)
		active := status == "queued" || status == "running" || status == "cancelling"
		timestamp := st.ColumnText(4)
		if active {
			timestamp = st.ColumnText(3)
		}
		t, parseErr := time.Parse(tsLayout, timestamp)
		if parseErr != nil {
			id := st.ColumnInt64(0)
			st.Close()
			return fmt.Errorf("job %d has invalid retention timestamp: %w", id, parseErr)
		}
		remove := !t.After(cutoff)
		if active && st.ColumnInt64(2) > 0 && alive(int(st.ColumnInt64(2))) {
			remove = false
		}
		log := st.ColumnText(5)
		if active && log != "" {
			// A detached runner owns these files until terminal collection commits.
			// This also protects them when that DB transaction is temporarily rejected.
			for _, suffix := range []string{".runner", ".result"} {
				if _, err := os.Stat(log + suffix); !os.IsNotExist(err) {
					remove = false
				}
			}
		}
		if remove {
			expired = append(expired, expiredJob{st.ColumnInt64(0), log})
		} else {
			keptLogs[filepath.Clean(log)] = true
		}
	}
	err = st.Err()
	st.Close()
	if err != nil {
		return err
	}
	for _, job := range expired {
		if err := s.execLocked(`UPDATE dispatches SET reply_to=NULL WHERE reply_to=?`, job.id); err != nil {
			return err
		}
		if err := s.execLocked(`DELETE FROM dispatches WHERE id=? AND kind='command'`, job.id); err != nil {
			return err
		}
	}
	if err := s.db.Exec("COMMIT"); err != nil {
		return err
	}
	committed = true
	for _, job := range expired {
		if job.log == "" || keptLogs[filepath.Clean(job.log)] {
			continue
		}
		if err := removeJobFiles(job.log); err != nil {
			return fmt.Errorf("remove job log %s: %w", job.log, err)
		}
	}
	entries, err := os.ReadDir(logDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(logDir, entry.Name())
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") || keptLogs[filepath.Clean(path)] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().After(cutoff) {
			if err := removeJobFiles(path); err != nil {
				return fmt.Errorf("remove orphan log %s: %w", path, err)
			}
		}
	}
	return nil
}

func removeJobFiles(log string) error {
	for _, suffix := range []string{"", ".runner", ".result", ".cancel"} {
		if err := os.Remove(log + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
