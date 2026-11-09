package server

import (
	"context"
	"fmt"
	"net"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type peerWatch struct {
	fd      int
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

type peerPoller struct {
	mu                  sync.Mutex
	queue               int
	wakeRead, wakeWrite int
	watches             map[int]*peerWatch
	err                 error
}

var sharedPeerPoller struct {
	once   sync.Once
	poller *peerPoller
	err    error
}

func getPeerPoller() (*peerPoller, error) {
	sharedPeerPoller.once.Do(func() {
		fds := []int{0, 0}
		if err := unix.Pipe(fds); err != nil {
			sharedPeerPoller.err = err
			return
		}
		for _, fd := range fds {
			unix.CloseOnExec(fd)
			if err := unix.SetNonblock(fd, true); err != nil {
				unix.Close(fds[0])
				unix.Close(fds[1])
				sharedPeerPoller.err = err
				return
			}
		}
		queue, err := newPeerEventQueue()
		if err != nil {
			unix.Close(fds[0])
			unix.Close(fds[1])
			sharedPeerPoller.err = err
			return
		}
		unix.CloseOnExec(queue)
		if err := addPeerEvent(queue, fds[0], true); err != nil {
			unix.Close(queue)
			unix.Close(fds[0])
			unix.Close(fds[1])
			sharedPeerPoller.err = err
			return
		}
		p := &peerPoller{queue: queue, wakeRead: fds[0], wakeWrite: fds[1], watches: make(map[int]*peerWatch)}
		sharedPeerPoller.poller = p
		go p.run()
	})
	return sharedPeerPoller.poller, sharedPeerPoller.err
}

func (p *peerPoller) wake() {
	for {
		if _, err := unix.Write(p.wakeWrite, []byte{1}); err != unix.EINTR {
			return
		}
	}
}

func (p *peerPoller) finish(w *peerWatch) {
	delete(p.watches, w.fd)
	deletePeerEvent(p.queue, w.fd)
	unix.Close(w.fd)
	w.cancel()
	close(w.done)
}

func (p *peerPoller) run() {
	for {
		ready, err := waitPeerEvents(p.queue)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			p.mu.Lock()
			p.err = err
			for _, w := range p.watches {
				p.finish(w)
			}
			p.mu.Unlock()
			return
		}
		for _, fd := range ready {
			if fd != p.wakeRead {
				continue
			}
			var buf [128]byte
			for {
				if _, err := unix.Read(p.wakeRead, buf[:]); err != nil && err != unix.EINTR {
					break
				}
			}
		}
		p.mu.Lock()
		for _, fd := range ready {
			if w := p.watches[fd]; w != nil {
				p.finish(w)
			}
		}
		for _, w := range p.watches {
			if w.stopped {
				p.finish(w)
			}
		}
		p.mu.Unlock()
	}
}

// peerContext observes full hangup without consuming persistent API input.
// All connections share one poll loop; a duplicate keeps each socket stable.
func peerContext(conn net.Conn) (context.Context, func(), error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return nil, nil, fmt.Errorf("API connection has no socket descriptor")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	fd := -1
	var dupErr error
	if err := raw.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		return nil, nil, err
	}
	if dupErr != nil {
		return nil, nil, dupErr
	}
	poller, err := getPeerPoller()
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &peerWatch{fd: fd, cancel: cancel, done: make(chan struct{})}
	poller.mu.Lock()
	if poller.err != nil {
		err = poller.err
	} else {
		err = addPeerEvent(poller.queue, fd, false)
	}
	if err != nil {
		poller.mu.Unlock()
		unix.Close(fd)
		cancel()
		return nil, nil, err
	}
	poller.watches[fd] = w
	poller.mu.Unlock()
	poller.wake()
	stop := func() {
		poller.mu.Lock()
		w.stopped = true
		poller.mu.Unlock()
		poller.wake()
		<-w.done
	}
	return ctx, stop, nil
}
