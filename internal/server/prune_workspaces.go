package server

import (
	"context"
	"os"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/tui"
)

// pruneViewers is called on the app loop. Inspect all selections and HQ
// projections so a second attached display can protect its workspace too.
func (s *Server) pruneViewers() []*tui.Viewer {
	s.mu.Lock()
	defer s.mu.Unlock()
	viewers := make([]*tui.Viewer, 0, len(s.clients))
	for client := range s.clients {
		if client.viewer != nil {
			viewers = append(viewers, client.viewer)
		}
	}
	return viewers
}

// Read dispatch state directly: the sidebar snapshot can lag job admission.
func (s *Server) pruneJobCwds() ([]string, error) {
	if _, err := os.Stat(dispatch.DBPath(s.session)); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	store, err := s.db()
	if err != nil {
		return nil, err
	}
	return store.ActiveBackgroundCwds()
}

type displayPruneScan struct {
	request uint64
	cancel  context.CancelFunc
}

func (c *clientConn) cancelPruneScan(request uint64) {
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()
	if scan := c.pruneScan; scan != nil && scan.request == request {
		c.pruneScan = nil
		scan.cancel()
	}
}

func (s *Server) pruneDisplayRequest(ctx context.Context, c *clientConn, m clientMsg) {
	reply := clientMsg{Type: m.Type + "_result", PruneRequest: m.PruneRequest}
	c.pruneMu.Lock()
	if c.pruneScan != nil || c.pruneExecuting {
		c.pruneMu.Unlock()
		reply.PruneError = "another prune operation is in progress"
		c.post(reply)
		return
	}

	if m.Type == endpoint.PruneScan {
		ctx, cancel := context.WithCancel(ctx)
		scan := &displayPruneScan{request: m.PruneRequest, cancel: cancel}
		c.pruneScan = scan
		c.pruneMu.Unlock()
		go func() {
			defer cancel()
			if s.closing.Load() != 0 {
				reply.PruneError = "server unavailable"
			} else {
				result := s.app.PruneScan(ctx, s.generation, s.pruneViewers)
				reply.PruneScan = &result
			}
			c.pruneMu.Lock()
			current := c.pruneScan == scan
			if current {
				c.pruneScan = nil
			}
			c.pruneMu.Unlock()
			if current && ctx.Err() == nil {
				c.post(reply)
			}
		}()
		return
	}

	c.pruneExecuting = true
	c.pruneMu.Unlock()
	go func() {
		if m.PruneExec == nil {
			reply.PruneError = "missing prune selection"
		} else {
			result := s.app.PruneExec(ctx, s.generation, *m.PruneExec, s.pruneViewers, func() bool {
				return s.closing.Load() == 0 && !s.inputHandoffFrozen && s.displayClientAttached(c)
			})
			reply.PruneResult = &result
		}
		c.pruneMu.Lock()
		c.pruneExecuting = false
		c.pruneMu.Unlock()
		c.post(reply)
	}()
}
