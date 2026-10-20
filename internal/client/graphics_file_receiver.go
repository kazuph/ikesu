package client

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
)

const graphicsFileReplyDrain = time.Second

type graphicsFileReceipt struct {
	identity endpoint.GraphicsTransmission
	started  bool
	early    *bool
}

type graphicsFileReceiver struct {
	mu       sync.Mutex
	pending  *graphicsFileReceipt
	draining map[uint32]time.Time
}

func (r *graphicsFileReceiver) prune(now time.Time) {
	for image, until := range r.draining {
		if !now.Before(until) {
			delete(r.draining, image)
		}
	}
}

func (r *graphicsFileReceiver) begin(out *os.File, message endpoint.GraphicsFile, allowed bool, now time.Time) (endpoint.GraphicsTransmission, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(now)
	identity := endpoint.GraphicsTransmission{TransferID: message.TransferID, ImageID: message.ImageID}
	if !allowed || r.pending != nil || !r.draining[message.ImageID].IsZero() {
		return identity, errors.New("graphics file is not owned by the active local viewer")
	}
	if err := writeGraphicsFile(out, message); err != nil {
		return identity, err
	}
	r.pending = &graphicsFileReceipt{identity: identity}
	return identity, nil
}

func (r *graphicsFileReceiver) finish(success bool, now time.Time) *endpoint.GraphicsTransmissionResult {
	identity := r.pending.identity
	r.pending = nil
	if r.draining == nil {
		r.draining = map[uint32]time.Time{}
	}
	r.draining[identity.ImageID] = now.Add(graphicsFileReplyDrain)
	return &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: success}
}

// Call only after Started has been queued/written to the server. A very
// fast terminal can reply before that write; retain rather than reorder it.
func (r *graphicsFileReceiver) started(identity endpoint.GraphicsTransmission, now time.Time) *endpoint.GraphicsTransmissionResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil || r.pending.identity != identity {
		return nil
	}
	r.pending.started = true
	if r.pending.early != nil {
		return r.finish(*r.pending.early, now)
	}
	return nil
}

func (r *graphicsFileReceiver) reply(raw []byte, now time.Time) (bool, *endpoint.GraphicsTransmissionResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(now)
	if r.pending != nil {
		if matched, success := endpoint.ParseGraphicsReply(raw, r.pending.identity.ImageID); matched {
			if !r.pending.started {
				if r.pending.early == nil {
					r.pending.early = &success
				}
				return true, nil
			}
			return true, r.finish(success, now)
		}
	}
	for image := range r.draining {
		if matched, _ := endpoint.ParseGraphicsReply(raw, image); matched {
			return true, nil
		}
	}
	return false, nil
}

func (r *graphicsFileReceiver) retired(identity endpoint.GraphicsTransmission, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending != nil && r.pending.identity == identity {
		r.finish(false, now)
		return true
	}
	return false
}

func (r *graphicsFileReceiver) disconnected(now time.Time) *endpoint.GraphicsTransmission {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending != nil {
		return &r.finish(false, now).GraphicsTransmission
	}
	return nil
}
