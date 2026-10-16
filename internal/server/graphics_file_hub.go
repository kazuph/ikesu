package server

import (
	"net"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/render"
)

type graphicsFileHub struct {
	source   *graphicsFileSource
	pending  *nativeGraphicsPending
	sequence uint64
	image    uint32
	streams  map[uint64]nativeGraphicsStream
}

type nativeGraphicsStream struct {
	conn   net.Conn
	viewer *clientConn
	image  uint32
}

type nativeGraphicsPending struct {
	identity    endpoint.GraphicsTransmission
	viewer      *clientConn
	pid         int
	placement   kitty.Placement
	started     chan struct{}
	result      chan bool
	delivery    <-chan clientWriteResult
	startedSeen bool
	resultSeen  bool
	drained     chan struct{}
	committed   bool
}

// Hub state and all viewer presentation state are confined to the app loop.
func (s *Server) nativeGraphicsViewer() *clientConn {
	var active *clientConn
	for _, c := range s.displayClients() {
		if c.viewer == nil {
			return nil
		}
		if !c.viewer.GraphicsActive() {
			continue
		}
		if active != nil {
			return nil
		}
		active = c
	}
	if active == nil || !active.directGraphics || active.directDisabled || active.graphics == nil || active.graphics.published == nil {
		return nil
	}
	return active
}

type graphicsPublishedSurface struct {
	panes  map[int]bool
	native map[uint32]bool
}

// Capture while the viewer's selection is active; render.Input.State belongs
// to the app loop and is restored/mutated after encoding this projection.
func graphicsSurface(in render.Input) *graphicsPublishedSurface {
	surface := &graphicsPublishedSurface{panes: map[int]bool{}, native: map[uint32]bool{}}
	if in.State != nil {
		for _, rect := range in.State.DisplayPaneRects() {
			if rect.Rect.W > 0 && rect.Rect.H > 0 && in.Panes[rect.ID] != nil {
				surface.panes[rect.ID] = true
			}
		}
		if in.State.PopupVisible() && in.Panes[in.State.Popup.PaneID] != nil {
			surface.panes[in.State.Popup.PaneID] = true
		}
	}
	for _, pane := range in.Panes {
		for _, placement := range pane.Graphics {
			if placement.Image.NativeID != 0 {
				surface.native[placement.Image.NativeID] = true
			}
		}
	}
	return surface
}

func surfaceHasPane(surface *graphicsPublishedSurface, pid int) bool {
	return surface != nil && surface.panes[pid]
}

func (s *Server) graphicsPaneVisible(pid int) bool {
	for _, c := range s.displayClients() {
		if c.viewer != nil && c.graphics != nil && c.viewer.DisplaysPane(s.app, pid) && surfaceHasPane(c.graphics.published, pid) {
			return true
		}
	}
	return false
}

func (s *Server) directGraphicsViewer(pid int, allowPending bool) *clientConn {
	c := s.nativeGraphicsViewer()
	if c == nil || !allowPending && s.graphicsFiles.pending != nil || !surfaceHasPane(c.graphics.published, pid) || !c.viewer.DisplaysPane(s.app, pid) {
		return nil
	}
	p := s.sup.Get(pid)
	if p == nil {
		return nil
	}
	cols, rows, cw, ch := c.viewer.GraphicsGeometry(s.app, pid)
	if !p.MatchesGraphicsGeometry(cols, rows, cw, ch) {
		return nil
	}
	return c
}

func (s *Server) graphicsSource() *graphicsFileSource {
	if s.graphicsFiles.source == nil {
		s.graphicsFiles.source, _ = newGraphicsFileSource()
	}
	if source := s.graphicsFiles.source; source != nil {
		for _, path := range []string{source.base, source.server, source.source} {
			if _, err := privateGraphicsDirectory(path); err != nil {
				return nil
			}
		}
	}
	return s.graphicsFiles.source
}

func (s *Server) graphicsTransmission(c *clientConn, message clientMsg) {
	pending := s.graphicsFiles.pending
	if pending == nil || pending.viewer != c {
		return
	}
	if message.Type == endpoint.GraphicsStartedMessage && message.GraphicsTransmission != nil && *message.GraphicsTransmission == pending.identity && !pending.startedSeen {
		pending.startedSeen = true
		close(pending.started)
	}
	if result := message.GraphicsResult; message.Type == endpoint.GraphicsResultMessage && result != nil && result.GraphicsTransmission == pending.identity && !pending.resultSeen && (!result.Success || pending.startedSeen) {
		pending.resultSeen = true
		pending.result <- result.Success
	}
}

func (s *Server) tickNativeGraphics() {
	viewer := s.nativeGraphicsViewer()
	for owner, stream := range s.graphicsFiles.streams {
		if stream.viewer != viewer {
			_ = stream.conn.Close()
			delete(s.graphicsFiles.streams, owner)
		}
	}
}

func (s *Server) nativeGraphicsInput(c *clientConn, in render.Input) render.Input {
	var panes map[int]*render.PaneSnap
	for pid, pane := range in.Panes {
		var filtered []kitty.Placement
		foreign := false
		for index, placement := range pane.Graphics {
			owned := placement.Image.NativeID == 0
			for _, stream := range s.graphicsFiles.streams {
				owned = owned || stream.viewer == c && stream.image == placement.Image.NativeID
			}
			if owned {
				if foreign {
					filtered = append(filtered, placement)
				}
			} else {
				if !foreign {
					filtered = append([]kitty.Placement(nil), pane.Graphics[:index]...)
					foreign = true
				}
			}
		}
		if foreign {
			if panes == nil {
				panes = make(map[int]*render.PaneSnap, len(in.Panes))
				for id, source := range in.Panes {
					panes[id] = source
				}
			}
			value := *pane
			value.Graphics = filtered
			panes[pid] = &value
		}
	}
	if panes != nil {
		in.Panes = panes
	}
	return in
}

// The terminal has placed this image already; publish its persistent descriptor
// before allowing the producer to close the stream and remove the layer.
func (s *Server) nativeGraphicsDrained(c *clientConn, surface *graphicsPublishedSurface) {
	p := s.graphicsFiles.pending
	if p == nil || p.viewer != c || !p.committed || p.drained == nil {
		return
	}
	if surface.native[p.identity.ImageID] {
		close(p.drained)
		p.drained = nil
	}
}

const (
	nativeGraphicsDeliveryWait = 5 * time.Second
	nativeGraphicsTerminalWait = 3 * time.Second
	nativeGraphicsAPIWait      = 9 * time.Second
)

func graphicsUnavailable(message string) *api.Error {
	return &api.Error{Code: "server_unavailable", Message: message}
}
