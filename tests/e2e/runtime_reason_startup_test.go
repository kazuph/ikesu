package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"strings"
)

func startRuntimeReasonSession(t *testing.T) *session {
	t.Helper()
	s := startEnv(t, map[string]string{"IKESU_LATENCY_TRACE": "1"})
	var startup bytes.Buffer
	s.observeOutput = func(data []byte) { startup.Write(data) }
	ready := false
	t.Cleanup(func() {
		if ready {
			return
		}
		t.Logf("startup terminal bytes=%d raw=%q", startup.Len(), startup.Bytes())
		dir := filepath.Join(s.stateDir, s.name)
		for _, path := range []string{api.LogPath(dir), filepath.Join(dir, "client.log")} {
			data, err := os.ReadFile(path)
			t.Logf("startup diagnostic %s error=%v:\n%s", path, err, data)
		}
	})
	began := time.Now()
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	ready = true
	s.observeOutput = nil
	t.Logf("startup prompt seconds=%.6f terminal_bytes=%d", time.Since(began).Seconds(), startup.Len())
	return s
}

// These predicates read real decision state over RPC. Check that state before
// draining terminal output; a screen refresh is not the readiness condition.
func (s *session) waitRuntimeReasonState(what string, timeout time.Duration, pred func(string) bool) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred(s.text()) {
			return
		}
		// Reuse pump's existing terminal polling cadence while the state is pending.
		next := time.Now().Add(20 * time.Millisecond)
		if deadline.Before(next) {
			next = deadline
		}
		s.pump(next)
	}
	s.t.Fatalf("timed out waiting for %s; screen:\n%s", what, s.text())
}
