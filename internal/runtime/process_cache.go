package runtime

import (
	"sync"
	"time"

	"github.com/kazuph/ikesu/internal/platform"
)

const foregroundJobCacheLifetime = 1500 * time.Millisecond

type foregroundCacheKey struct {
	shellPID, groupID   int
	shellCwd, leaderCwd string
	leaderName          string
}

type foregroundCache struct {
	mu     sync.Mutex
	key    foregroundCacheKey
	readAt time.Time
	job    platform.ForegroundJob
}

func (p *Pane) cachedForegroundJob() platform.ForegroundJob {
	job, _, _ := p.cachedForegroundScan()
	return job
}

// cachedForegroundScan numbers the scan while the cache lock fixes the read
// order, so a later number always carries a reading taken no earlier. The
// leader cwd is read under the same lock for the same reason.
func (p *Pane) cachedForegroundScan() (platform.ForegroundJob, string, uint64) {
	c := &p.processCache
	c.mu.Lock()
	defer c.mu.Unlock()
	key := foregroundCacheKey{
		shellPID: p.ShellPID(),
		groupID:  platform.ForegroundPID(p.ptyFD),
	}
	// Cheap identity/cwd probes stay current even while the group scan is cached.
	key.shellCwd = platform.ProcCwd(key.shellPID)
	key.leaderCwd = platform.ProcCwd(key.groupID)
	key.leaderName = platform.ProcName(key.groupID)
	now := time.Now()
	if key != c.key || now.Sub(c.readAt) >= foregroundJobCacheLifetime {
		c.job = platform.ForegroundJobOf(p.ptyFD)
		c.key, c.readAt = key, now
	}
	cwd := key.leaderCwd
	if c.job.ProcessGroupID != key.groupID {
		cwd = platform.ProcCwd(c.job.ProcessGroupID)
	}
	return c.job, cwd, p.processPollSeq.Add(1)
}
