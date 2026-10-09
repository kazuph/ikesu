package server

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kazuph/goterrace/internal/dispatch"
)

const jobCleanupInterval = 10 * time.Minute
const jobActiveRefresh = time.Second
const jobIdleRefresh = 10 * time.Second

type jobCache struct {
	mu     sync.RWMutex
	jobs   []dispatch.Job
	alive  map[string]bool
	wake   chan struct{}
	readAt time.Time
	err    error
}

type databaseStamp struct {
	exists   bool
	size     int64
	modified time.Time
}

func jobDatabaseStamp(session string) [2]databaseStamp {
	var result [2]databaseStamp
	for i, path := range []string{dispatch.DBPath(session), dispatch.DBPath(session) + "-wal"} {
		if info, err := os.Stat(path); err == nil {
			result[i] = databaseStamp{true, info.Size(), info.ModTime()}
		}
	}
	return result
}

func jobsRefreshInterval(jobs []dispatch.Job, now time.Time) time.Duration {
	for _, job := range jobs {
		if job.Status == "queued" || job.Status == "running" || job.Status == "cancelling" || job.FinishedMs != nil && now.Sub(time.UnixMilli(*job.FinishedMs)) <= time.Minute {
			return jobActiveRefresh
		}
	}
	return jobIdleRefresh
}

func (s *Server) cachedJobSnapshot() ([]dispatch.Job, map[string]bool) {
	s.jobCache.mu.RLock()
	defer s.jobCache.mu.RUnlock()
	return s.jobCache.jobs, s.jobCache.alive
}

func (s *Server) invalidateJobCache() {
	if s.jobCache.wake == nil {
		return
	}
	select {
	case s.jobCache.wake <- struct{}{}:
	default:
	}
}

func (s *Server) jobCacheLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var store *dispatch.Store
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	var previous [2]databaseStamp
	var lastRead, lastCleanup time.Time
	interval := jobIdleRefresh
	refresh := func(force bool) {
		now := time.Now()
		stamp := jobDatabaseStamp(s.session)
		if !force && stamp == previous && now.Sub(lastRead) < interval {
			return
		}
		if !stamp[0].exists {
			previous, lastRead = stamp, now
			return
		}
		var err error
		if store == nil {
			store, err = dispatch.Open(dispatch.DBPath(s.session))
		}
		if err == nil && now.Sub(lastCleanup) >= jobCleanupInterval {
			err = store.CleanupJobs(now, dispatch.JobLogDir(s.session), processGroupAlive)
			if err == nil {
				lastCleanup = now
			}
		}
		var jobs []dispatch.Job
		if err == nil {
			jobs, err = store.CommandRows(0)
		}
		if err != nil {
			s.jobCache.mu.Lock()
			s.jobCache.err = err
			s.jobCache.mu.Unlock()
			fmt.Fprintf(os.Stderr, "gtx job snapshot/cleanup: %v\n", err)
			return
		}
		alive := map[string]bool{}
		for _, job := range jobs {
			if (job.Status == "running" || job.Status == "cancelling") && job.RunnerPID != nil {
				alive[job.ID] = processGroupAlive(int(*job.RunnerPID))
			}
		}
		previous, lastRead = jobDatabaseStamp(s.session), now
		interval = jobsRefreshInterval(jobs, now)
		s.jobCache.mu.Lock()
		s.jobCache.jobs, s.jobCache.alive, s.jobCache.readAt, s.jobCache.err = jobs, alive, now, nil
		s.jobCache.mu.Unlock()
		s.app.Do(s.app.RefreshJobs)
	}
	refresh(true)
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			refresh(false)
		case <-s.jobCache.wake:
			refresh(true)
		}
	}
}
