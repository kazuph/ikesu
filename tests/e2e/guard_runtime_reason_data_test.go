package e2e

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestRuntimeCurlReasonNameInData(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "runtime-curl-test", Agent: "claude", State: "idle"}, nil)
	root := s.stateDir
	script := filepath.Join(root, "stop-hook.sh")
	content := "CCORD_WEBHOOK_URL='" + server.URL + "'\nINPUT='{\"text\":\"AI_GUARD_REQUEST_REASON\"}'\ncurl -s -X POST \"$CCORD_WEBHOOK_URL\" -H \"Content-Type: application/json\" -d \"$INPUT\" --connect-timeout 2 --max-time 5 > /dev/null 2>&1\n"
	if err := os.WriteFile(script, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.Env = []string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_CONFIG=" + filepath.Join(root, "no-config"), "IKESU_STATE_DIR=" + root, "IKESU_PANE_ID=p1", "IKESU_GUARD_MARKER=" + filepath.Join(root, "guard", "agent-panes", "p_1.json"), "BASH_ENV=" + filepath.Join(root, "guard", "shell", "p_1", "bash-env")}
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("stop hook failed: %v: %s", err, output.String())
	}
	select {
	case body := <-received:
		if body != `{"text":"AI_GUARD_REQUEST_REASON"}` {
			t.Fatalf("unexpected body: %q", body)
		}
	case <-ctx.Done():
		t.Fatal("local server did not receive request")
	}
	var decisions api.DecisionListResult
	s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &decisions)
	if len(decisions.Decisions) != 0 {
		t.Fatalf("unexpected decisions: %+v", decisions.Decisions)
	}
}
