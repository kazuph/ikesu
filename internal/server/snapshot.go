package server

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/snapshot"
)

var errSnapshotPaneNotFound = errors.New("pane not found")

func (s *Server) paneSnapshot(ctx context.Context, req *api.Request) api.Response {
	var params api.PaneSnapshotParams
	if err := req.DecodeParams(&params); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if params.Mode != "last_turn" && params.Mode != "handoff" {
		return api.Failure(req.ID, api.ErrInvalidRequest, "mode must be last_turn or handoff")
	}
	lines, maxChars := snapshot.DefaultLines, snapshot.DefaultMaxChars
	if params.Lines != nil {
		if *params.Lines == 0 || *params.Lines > snapshot.MaxLines {
			return api.Failure(req.ID, api.ErrInvalidRequest, "lines must be between 1 and 5000")
		}
		lines = int(*params.Lines)
	}
	if params.MaxChars != nil {
		if *params.MaxChars < 500 || *params.MaxChars > 100000 {
			return api.Failure(req.ID, api.ErrInvalidRequest, "max_chars must be between 500 and 100000")
		}
		maxChars = int(*params.MaxChars)
	}
	if params.Mode == "last_turn" && params.SaveLog {
		return api.Failure(req.ID, api.ErrInvalidRequest, "save_log is only supported in handoff mode")
	}
	if params.PaneID == "" {
		if params.CallerProcessID == nil {
			return api.Failure(req.ID, api.ErrPaneNotFound, "caller process identity is required")
		}
		var pid int
		var callerErr *api.Error
		if !s.app.DoContext(ctx, func() {
			pid, callerErr = s.resolveCallerPane(params.CallerPaneID, params.CallerProcessID)
		}) {
			return api.Failure(req.ID, api.ErrPaneNotFound, "could not resolve the caller pane")
		}
		if callerErr != nil {
			return api.Failure(req.ID, callerErr.Code, callerErr.Message)
		}
		params.PaneID = fmtPaneID(pid)
	}
	result, err := s.buildSnapshot(ctx, params.PaneID, params.Mode, lines, maxChars, params.SaveLog)
	switch {
	case errors.Is(err, errSnapshotPaneNotFound):
		return s.paneNotFound(req.ID, params.PaneID)
	case err != nil:
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	default:
		return api.Success(req.ID, api.PaneSnapshotResult[snapshot.Result]{Type: "pane_snapshot", Snapshot: result})
	}
}

func (s *Server) buildSnapshot(ctx context.Context, paneID, mode string, lines, maxChars int, saveLog bool) (snapshot.Result, error) {
	in := snapshot.Input{Mode: mode, FallbackLines: lines, MaxChars: maxChars}
	var pane *runtime.Pane
	ok := s.app.DoContext(ctx, func() {
		pid, wsIdx, _, ok := s.findPane(paneID)
		if !ok {
			return
		}
		pane = s.app.Supervisor().Get(pid)
		if pane == nil {
			return
		}
		ws := s.app.State().Workspaces[wsIdx]
		in.Workspace = s.app.WorkspaceLabel(wsIdx)
		in.PaneID = fmtPaneID(pid)
		sn := pane.Snapshot()
		in.Agent = sn.AgentID
		if agent, _, ok := s.app.Reported(pid); ok && agent != "" {
			in.Agent = agent
		}
		in.Cwd = sn.Cwd
		if in.Cwd == "" {
			in.Cwd = ws.Cwd
		}
	})
	if !ok {
		if err := ctx.Err(); err != nil {
			return snapshot.Result{}, err
		}
		return snapshot.Result{}, errors.New("server is stopping")
	}
	if pane == nil {
		return snapshot.Result{}, errSnapshotPaneNotFound
	}
	rows, err := pane.ReadTailLines("recent-unwrapped", true, snapshot.MaxLines)
	if err != nil {
		return snapshot.Result{}, err
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	in.Lines = rows

	var saveErr error
	if saveLog {
		in.SaveLog = func(text string) (string, error) {
			path, err := saveSnapshotLog(in.PaneID, text)
			if err != nil {
				saveErr = fmt.Errorf("save snapshot log: %w", err)
			}
			return path, err
		}
	}
	result := snapshot.Build(ctx, in)
	return result, saveErr
}

func saveSnapshotLog(paneID, text string) (string, error) {
	file, err := os.CreateTemp("", "ikesu-snapshot-"+paneID+"-*.log")
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func (s *Server) handoffSnapshotMarkdown(pid int) (string, error) {
	result, err := s.buildSnapshot(context.Background(), fmtPaneID(pid), "handoff", snapshot.DefaultLines, snapshot.DefaultMaxChars, false)
	return result.Markdown, err
}
