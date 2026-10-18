package server

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"golang.org/x/sys/unix"
)

func graphicsProducerConnected(conn net.Conn) bool {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return false
	}
	live := false
	err = raw.Control(func(fd uintptr) {
		var data [1]byte
		n, _, err := unix.Recvfrom(int(fd), data[:], unix.MSG_PEEK|unix.MSG_DONTWAIT)
		live = n > 0 || err == unix.EAGAIN || err == unix.EWOULDBLOCK
	})
	return err == nil && live
}

func (s *Server) serveGraphicsFile(conn net.Conn, pid int, frame api.PaneGraphicsStreamFrame, owner uint64) *api.Error {
	var source *graphicsFileSource
	var expected uint64
	var failure *api.Error
	s.app.Do(func() {
		expected, failure = graphicsFileFrameBytes(frame, s.directGraphicsViewer(pid, false) != nil)
		source = s.graphicsFiles.source
	})
	if failure != nil {
		return failure
	}
	if source == nil {
		return &api.Error{Code: "invalid_frame_file", Message: "no advertised graphics source directory"}
	}
	file, err := source.open(frame.File.Path, expected)
	if err != nil {
		return &api.Error{Code: "invalid_frame_file", Message: err.Error()}
	}
	defer file.file.Close()
	var pending *nativeGraphicsPending
	s.app.Do(func() {
		viewer := s.directGraphicsViewer(pid, false)
		if viewer == nil || frame.Format != "rgba" || frame.LayerID != nil && *frame.LayerID != "primary" {
			_, failure = graphicsFileFrameBytes(frame, false)
			return
		}
		hub := &s.graphicsFiles
		hub.sequence++
		hub.image++
		identity := endpoint.GraphicsTransmission{TransferID: hub.sequence, ImageID: ^uint32(0) - hub.image}
		placement, replace, rejected := s.sup.NativeGraphicsPlacement(pid, frame.PaneGraphicsSetParams, identity.ImageID, owner)
		if rejected != nil {
			failure = rejected
			return
		}
		asset, err := json.Marshal(endpoint.GraphicsSurfaceAsset{Pane: pid, ReplaceImage: replace, Placement: placement})
		if err != nil {
			failure = graphicsUnavailable(err.Error())
			return
		}
		pending = &nativeGraphicsPending{identity: identity, viewer: viewer, pid: pid, placement: placement,
			started: make(chan struct{}), result: make(chan bool, 1), drained: make(chan struct{})}
		message := clientMsg{Type: endpoint.GraphicsFileMessage, FocusEpoch: viewer.viewer.FocusEpoch(), GraphicsFile: &endpoint.GraphicsFile{
			Path: frame.File.Path, ExpectedLen: expected, ImageID: identity.ImageID, TransferID: identity.TransferID,
			Control: fmt.Sprintf("a=t,f=32,s=%d,v=%d,i=%d,q=0", frame.ImageWidth, frame.ImageHeight, identity.ImageID), SurfaceAsset: asset}}
		pending.delivery, err = viewer.queueGraphicsControl(message)
		if err != nil {
			failure = graphicsUnavailable(err.Error())
			pending = nil
			return
		}
		hub.pending = pending
	})
	if failure != nil {
		return failure
	}
	if pending == nil {
		return s.installGraphicsFilePixels(pid, frame, file, owner, false)
	}
	deadline := time.Now().Add(nativeGraphicsAPIWait)
	success, failure := waitNativeGraphics(pending, deadline)
	if failure == nil && success {
		if err := file.check(); err != nil || !graphicsProducerConnected(conn) {
			failure = graphicsUnavailable("graphics source or producer changed before receipt")
		} else {
			drained := pending.drained
			s.app.Do(func() {
				if s.graphicsFiles.pending != pending || s.directGraphicsViewer(pid, true) != pending.viewer {
					failure = graphicsUnavailable("native graphics viewer no longer owns the surface")
					return
				}
				failure = s.sup.SetNativeGraphicsLayer(pid, frame.PaneGraphicsSetParams, pending.placement, owner)
				if failure == nil {
					pending.committed = true
					if s.graphicsFiles.streams == nil {
						s.graphicsFiles.streams = map[uint64]nativeGraphicsStream{}
					}
					s.graphicsFiles.streams[owner] = nativeGraphicsStream{conn: conn, viewer: pending.viewer, image: pending.identity.ImageID}
					pending.viewer.viewer.Redraw(s.app)
				}
			})
			if failure == nil {
				select {
				case <-drained:
				case <-pending.viewer.done:
					failure = graphicsUnavailable("graphics viewer disconnected before projection drain")
				case <-time.After(time.Until(deadline)):
					failure = graphicsUnavailable("native graphics projection was not delivered")
				}
			}
		}
	}
	s.app.Do(func() {
		if s.graphicsFiles.pending == pending {
			s.graphicsFiles.pending = nil
		}
		if failure != nil || !success {
			pending.viewer.directDisabled = true
			if _, err := pending.viewer.queueGraphicsControl(clientMsg{Type: endpoint.GraphicsRetiredMessage, GraphicsTransmission: &pending.identity}); err != nil {
				go pending.viewer.close()
			}
		}
	})
	if failure != nil {
		return failure
	}
	if !success {
		return s.installGraphicsFilePixels(pid, frame, file, owner, true)
	}
	if err := file.check(); err != nil || !graphicsProducerConnected(conn) {
		return graphicsUnavailable("graphics source or producer changed before acknowledgement")
	}
	return nil
}

func waitNativeGraphics(p *nativeGraphicsPending, deadline time.Time) (bool, *api.Error) {
	timer := time.NewTimer(nativeGraphicsDeliveryWait)
	defer timer.Stop()
	overall := time.NewTimer(time.Until(deadline))
	defer overall.Stop()
	started, delivered := p.started, p.delivery
	for {
		select {
		case <-started:
			started = nil
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(nativeGraphicsTerminalWait)
		case success := <-p.result:
			return success, nil
		case result := <-delivered:
			delivered = nil
			if result.err != nil {
				return false, graphicsUnavailable("native graphics delivery failed")
			}
		case <-timer.C:
			return false, graphicsUnavailable("native graphics receipt expired")
		case <-overall.C:
			return false, graphicsUnavailable("native graphics API wait expired")
		case <-p.viewer.done:
			return false, graphicsUnavailable("native graphics viewer disconnected")
		}
	}
}

func (s *Server) installGraphicsFilePixels(pid int, frame api.PaneGraphicsStreamFrame, file *graphicsSourceFile, owner uint64, afterRejection bool) *api.Error {
	pixels, err := file.ownedPixels()
	if err != nil {
		if afterRejection {
			return graphicsUnavailable(err.Error())
		}
		return &api.Error{Code: "invalid_frame_file", Message: err.Error()}
	}
	failure := graphicsUnavailable("session stopped before file installation")
	s.app.Do(func() {
		failure = s.sup.SetGraphicsLayer(pid, frame.PaneGraphicsSetParams, pixels, owner)
		if failure == nil && (frame.LayerID == nil || *frame.LayerID == "primary") {
			delete(s.graphicsFiles.streams, owner)
		}
	})
	if failure != nil && afterRejection {
		return graphicsUnavailable(failure.Message)
	}
	return failure
}
