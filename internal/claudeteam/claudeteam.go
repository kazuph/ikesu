// Package claudeteam reads Claude Code agent-team configs
// (<config dir>/teams/<team>/config.json) to tell whether every teammate of
// a lead process has gone idle.
//
// Claude Code keeps teammates alive after they finish and, while any
// teammate exists, keeps the busy half-circle in the terminal title. The
// title alone therefore cannot tell a lead waiting for input from a lead
// whose teammates are working. Each team config names its lead process
// (leadProcess.pid) and marks every teammate with isActive, which turns
// false when the teammate finishes its turn.
package claudeteam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// refreshInterval bounds how often Refresh touches the file system.
const refreshInterval = time.Second

// watchTTL drops a lead that detection has stopped asking about.
const watchTTL = 30 * time.Second

type config struct {
	LeadAgentID string `json:"leadAgentId"`
	LeadProcess struct {
		PID      int    `json:"pid"`
		PIDSpace string `json:"pidSpace"`
	} `json:"leadProcess"`
	CreatedAt int64 `json:"createdAt"`
	Members   []struct {
		AgentID   string `json:"agentId"`
		AgentType string `json:"agentType"`
		IsActive  *bool  `json:"isActive"`
	} `json:"members"`
}

// idle reports whether cfg has at least one teammate and every teammate
// says isActive=false. A teammate without the flag counts as busy.
func (cfg config) idle() bool {
	teammates := 0
	for _, m := range cfg.Members {
		if m.AgentType == "team-lead" || cfg.LeadAgentID != "" && m.AgentID == cfg.LeadAgentID {
			continue
		}
		teammates++
		if m.IsActive == nil || *m.IsActive {
			return false
		}
	}
	return teammates > 0
}

// readConfig parses one team config file.
func readConfig(path string) (config, error) {
	var cfg config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(info os.FileInfo) fileStamp {
	return fileStamp{mod: info.ModTime(), size: info.Size()}
}

type watch struct {
	paneID int
	root   string
	seen   time.Time
}

type leadFile struct {
	path    string
	stamp   fileStamp
	created int64
	idle    bool
}

type rootIndex struct {
	dirStamp fileStamp
	scanned  bool
	// byPID maps a lead PID to the newest team config naming it.
	byPID map[int]leadFile
}

// Watcher caches team state per lead PID. Watch and Idle are cheap and
// safe on the UI loop; Refresh does the file I/O and belongs off it.
type Watcher struct {
	host string

	mu      sync.Mutex
	watches map[int]watch // lead PID → who asked
	idle    map[int]bool  // lead PID → every teammate idle
	last    time.Time

	// roots is touched only by Refresh, which runs on one goroutine.
	roots map[string]*rootIndex
}

// NewWatcher returns a Watcher for lead processes on this host.
func NewWatcher() *Watcher {
	host, _ := os.Hostname()
	return &Watcher{host: host, watches: map[int]watch{}, idle: map[int]bool{}, roots: map[string]*rootIndex{}}
}

// Watch asks Refresh to track the team led by pid, whose Claude config
// directory is root (normally ~/.claude).
func (w *Watcher) Watch(paneID, pid int, root string, now time.Time) {
	if w == nil || pid <= 0 || root == "" {
		return
	}
	w.mu.Lock()
	w.watches[pid] = watch{paneID: paneID, root: root, seen: now}
	w.mu.Unlock()
}

// Idle reports the last refreshed state: true only when a team config
// names pid as its lead and all of its teammates are idle.
func (w *Watcher) Idle(pid int) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.idle[pid]
}

// Refresh re-reads the watched teams at most once per refreshInterval and
// returns the panes whose idle state changed.
func (w *Watcher) Refresh(now time.Time) []int {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if !w.last.IsZero() && now.Sub(w.last) < refreshInterval {
		w.mu.Unlock()
		return nil
	}
	w.last = now
	var changed []int
	watches := map[int]watch{}
	for pid, wt := range w.watches {
		if now.Sub(wt.seen) > watchTTL {
			delete(w.watches, pid)
			if w.idle[pid] {
				changed = append(changed, wt.paneID)
			}
			delete(w.idle, pid)
			continue
		}
		watches[pid] = wt
	}
	w.mu.Unlock()
	if len(watches) == 0 {
		return changed
	}

	next := map[int]bool{}
	for pid, wt := range watches {
		idx := w.index(wt.root)
		lf, ok := idx.byPID[pid]
		if !ok {
			continue
		}
		info, err := os.Stat(lf.path)
		if err != nil {
			delete(idx.byPID, pid)
			continue
		}
		if st := stampOf(info); st != lf.stamp {
			cfg, err := readConfig(lf.path)
			if err != nil || cfg.LeadProcess.PID != pid {
				delete(idx.byPID, pid)
				continue
			}
			lf.stamp, lf.idle = st, cfg.idle()
			idx.byPID[pid] = lf
		}
		next[pid] = lf.idle
	}

	w.mu.Lock()
	for pid, wt := range watches {
		if _, still := w.watches[pid]; !still {
			continue
		}
		if w.idle[pid] != next[pid] {
			changed = append(changed, wt.paneID)
		}
		if next[pid] {
			w.idle[pid] = true
		} else {
			delete(w.idle, pid)
		}
	}
	w.mu.Unlock()
	return changed
}

// index returns root's lead-PID index, rescanning the teams directory when
// a team was created or removed since the last scan.
func (w *Watcher) index(root string) *rootIndex {
	idx := w.roots[root]
	if idx == nil {
		idx = &rootIndex{byPID: map[int]leadFile{}}
		w.roots[root] = idx
	}
	dir := filepath.Join(root, "teams")
	info, err := os.Stat(dir)
	if err != nil {
		idx.byPID = map[int]leadFile{}
		idx.scanned, idx.dirStamp = false, fileStamp{}
		return idx
	}
	st := stampOf(info)
	if idx.scanned && st == idx.dirStamp {
		return idx
	}
	idx.dirStamp, idx.scanned = st, true
	byPID := map[int]leadFile{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "config.json")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		cfg, err := readConfig(path)
		if err != nil || cfg.LeadProcess.PID <= 0 {
			continue
		}
		if cfg.LeadProcess.PIDSpace != "" && w.host != "" && cfg.LeadProcess.PIDSpace != w.host {
			continue
		}
		lf := leadFile{path: path, stamp: stampOf(info), created: cfg.CreatedAt, idle: cfg.idle()}
		if old, ok := byPID[cfg.LeadProcess.PID]; ok && old.created > lf.created {
			continue
		}
		byPID[cfg.LeadProcess.PID] = lf
	}
	idx.byPID = byPID
	return idx
}
