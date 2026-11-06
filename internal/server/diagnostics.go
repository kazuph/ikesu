package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// Diagnostics files are created exclusively in the private session directory.
func (s *Server) diagnostics(req *api.Request) api.Response {
	var p api.DiagnosticsParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	if filepath.IsAbs(p.Name) && filepath.Dir(filepath.Clean(p.Name)) == filepath.Clean(s.dir) {
		p.Name = filepath.Base(p.Name)
	}
	if p.Name == "" || filepath.Base(p.Name) != p.Name || p.Name == "." || p.Name == ".." {
		return api.Failure(req.ID, api.ErrInvalidRequest, "out must name a file directly in the session state directory")
	}
	var duration time.Duration
	if req.Method == api.MethodServerProfile {
		var err error
		duration, err = time.ParseDuration(p.CPU)
		if err != nil || duration <= 0 {
			return api.Failure(req.ID, api.ErrInvalidRequest, "cpu must be a positive duration")
		}
	} else if p.CPU != "" {
		return api.Failure(req.ID, api.ErrInvalidRequest, "cpu is only valid for profile")
	}
	path := filepath.Join(s.dir, p.Name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return api.Failure(req.ID, "diagnostic_error", err.Error())
	}
	if req.Method == api.MethodServerProfile {
		err = pprof.StartCPUProfile(f)
		if err == nil {
			timer := time.NewTimer(duration)
			select {
			case <-timer.C:
			case <-s.app.Done():
				err = errors.New("server stopped before profiling completed")
			}
			timer.Stop()
			pprof.StopCPUProfile()
		}
	} else {
		err = writeStacks(f)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return api.Failure(req.ID, "diagnostic_error", fmt.Sprint(err))
	}
	return api.Success(req.ID, map[string]any{"type": "diagnostic", "path": path})
}

// Go's debug stack includes argument words, which can contain sensitive data.
// Retain goroutine state and source locations while removing argument values.
func writeStacks(f *os.File) error {
	// runtime/pprof's text dump truncates at 64 MiB. Grow until the
	// runtime reports that the complete goroutine snapshot fits.
	size := 1 << 20
	var snapshot []byte
	for {
		snapshot = make([]byte, size)
		n := runtime.Stack(snapshot, true)
		if n < len(snapshot) {
			snapshot = snapshot[:n]
			break
		}
		size *= 2
	}
	for _, line := range strings.Split(string(snapshot), "\n") {
		if !strings.HasPrefix(line, "goroutine ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "created by ") {
			for i := 0; i < len(line); i++ {
				if line[i] != '(' {
					continue
				}
				if i > 0 && line[i-1] == '.' {
					if end := strings.IndexByte(line[i:], ')'); end >= 0 {
						i += end
						continue
					}
				}
				line = line[:i] + "(...)"
				break
			}
		}
		if _, err := fmt.Fprintln(f, line); err != nil {
			return err
		}
	}
	return nil
}
