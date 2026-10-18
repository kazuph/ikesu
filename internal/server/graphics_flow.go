package server

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
)

// P4b bounds image receipt waits independently for every viewer.
const graphicsReceiptWait = 500 * time.Millisecond

type graphicsFlow struct {
	published          *graphicsPublishedSurface
	encoder            endpoint.SnapshotEncoder
	revision, awaiting uint64
	pending            bool
	writing            bool
	until              time.Time
}

func (s *Server) installGraphicsSnapshotSink(c *clientConn, peer endpoint.Peer) {
	flow := &graphicsFlow{}
	flow.encoder.GraphicsPatches = endpoint.HasGraphicsPatches(peer) && endpoint.HasGraphicsPatches(s.endpointPeer)
	c.graphics = flow
	c.viewer.SetSnapshotSink(func(in render.Input) error {
		if flow.writing || flow.awaiting != 0 && time.Now().Before(flow.until) {
			flow.pending = true
			return nil
		}
		flow.awaiting = 0
		in = s.nativeGraphicsInput(c, in)
		surface := graphicsSurface(in)
		flow.pending = false
		flow.revision++
		if flow.encoder.GraphicsPatches {
			in.SurfaceRevision = flow.revision
		}
		data, err := flow.encoder.Encode(in)
		if err != nil {
			_ = c.send(clientMsg{Type: "error", Reason: "endpoint_snapshot_invalid: " + err.Error()})
			s.dropConn(c)
			return err
		}
		message := clientMsg{Type: "snapshot", Data: encode64(data), RequestAck: c.viewer.AwaitingFocusRender(), FocusEpoch: c.viewer.FocusEpoch()}
		flow.writing = true
		// GraphicsPatches is added to the wire copy by the encoder.
		if flow.encoder.SentGraphicsPatch() {
			flow.awaiting = flow.revision
		}
		revision := flow.revision
		reply, err := c.queueGraphicsFrame(message)
		if err != nil {
			flow.writing = false
			return err
		}
		go func() {
			var result clientWriteResult
			select {
			case result = <-reply:
			case <-c.done:
				result.err = net.ErrClosed
			}
			s.app.Do(func() {
				flow.writing = false
				if result.err != nil {
					s.dropConn(c)
					return
				}
				flow.published = surface
				s.nativeGraphicsDrained(c, surface)
				if flow.awaiting == revision {
					s.armGraphicsReceipt(c, revision)
				} else if flow.pending {
					flow.pending = false
					c.viewer.Redraw(s.app)
				}
			})
		}()
		return nil
	})
}

func (s *Server) armGraphicsReceipt(c *clientConn, revision uint64) {
	flow := c.graphics
	flow.until = time.Now().Add(graphicsReceiptWait)
	time.AfterFunc(graphicsReceiptWait, func() {
		s.app.Do(func() {
			s.mu.Lock()
			live := s.clients[c]
			s.mu.Unlock()
			if live && flow.awaiting == revision {
				s.releaseGraphicsReceipt(c)
			}
		})
	})
}

func (s *Server) releaseGraphicsReceipt(c *clientConn) {
	flow := c.graphics
	flow.awaiting = 0
	flow.until = time.Time{}
	if flow.pending {
		flow.pending = false
		c.viewer.Redraw(s.app)
	}
}

func (s *Server) graphicsControl(c *clientConn, message clientMsg) {
	flow := c.graphics
	if flow == nil || !flow.encoder.GraphicsPatches {
		_ = c.send(clientMsg{Type: "error", Reason: "endpoint_action_unsupported: graphics patches"})
		return
	}
	switch message.Kind {
	case endpoint.GraphicsPatchAck:
		revision, err := strconv.ParseUint(message.Data, 10, 64)
		if err != nil {
			_ = c.send(clientMsg{Type: "error", Reason: "invalid graphics receipt revision"})
			return
		}
		if flow.awaiting != 0 && revision == flow.awaiting {
			s.releaseGraphicsReceipt(c)
		}
	case endpoint.GraphicsPatchMiss:
		// The observed contract intentionally ignores miss payload contents.
		flow.awaiting = 0
		flow.until = time.Time{}
		flow.pending = false
		flow.encoder.ResetGraphics()
		c.viewer.Redraw(s.app)
	default:
		_ = c.send(clientMsg{Type: "error", Reason: fmt.Sprintf("endpoint_action_unsupported: %s", message.Kind)})
	}
}
