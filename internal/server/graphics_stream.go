package server

import (
	"bufio"
	"io"
	"net"

	"github.com/kazuph/goterrace/internal/api"
)

func (s *Server) serveGraphicsStream(conn net.Conn, reader *bufio.Reader, req *api.Request) {
	var target api.PaneGraphicsClearParams
	if err := req.DecodeParams(&target); err != nil {
		_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest, err.Error()))
		return
	}
	var opened api.Response
	pid := -1
	s.app.Do(func() {
		opened = s.graphicsRequest(req)
		pid, _, _, _ = s.findPane(target.PaneID)
	})
	if err := api.WriteResponse(conn, opened); err != nil || opened.Err != nil {
		return
	}
	owner := s.graphicsStreamOwner.Add(1)
	layers := map[string]*string{}
	defer s.app.Do(func() {
		if pane := s.sup.Get(pid); pane != nil {
			for _, id := range layers {
				pane.ClearGraphicsLayer(id, owner)
			}
		}
	})
	fail := func(message string) {
		_ = api.WriteResponse(conn, api.Failure(req.ID, "invalid_frame", message))
	}
	for {
		frame, err := api.ReadGraphicsFrame(reader)
		if err != nil {
			if err != io.EOF {
				fail(err.Error())
			}
			return
		}
		if frame.File != nil {
			fail("file frames are not supported")
			return
		}
		if frame.DataLength == nil {
			fail("frame requires data_length or file")
			return
		}
		if *frame.DataLength > uint64(^uint(0)>>1) {
			fail("frame data length exceeds addressable memory")
			return
		}
		data, err := io.ReadAll(io.LimitReader(reader, int64(*frame.DataLength)))
		if err != nil || uint64(len(data)) != *frame.DataLength {
			fail("incomplete frame data")
			return
		}
		if frame.LayerID == nil {
			frame.LayerID = target.LayerID
		}
		var failure *api.Error
		s.app.Do(func() {
			pane := s.sup.Get(pid)
			if pane == nil {
				failure = &api.Error{Code: api.ErrPaneNotFound, Message: "stream pane no longer exists"}
				return
			}
			failure = pane.SetGraphicsLayer(frame.PaneGraphicsSetParams, data, owner)
		})
		if failure != nil {
			_ = api.WriteResponse(conn, api.Failure(req.ID, failure.Code, failure.Message))
			return
		}
		key := "primary"
		if frame.LayerID != nil {
			key = *frame.LayerID
		}
		layers[key] = frame.LayerID
	}
}
