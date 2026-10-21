package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestConfigAllowNestedWithRealServerAncestor(t *testing.T) {
	teststate.Lock(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(cfg, []byte(fmt.Sprintf("onboarding=false\n[experimental]\nallow_nested=%t\n[session]\nresume_agents_on_restore=false\n", allow)), 0600); err != nil {
				t.Fatal(err)
			}
			s := startEnv(t, map[string]string{"GTX_CONFIG": cfg})
			s.waitFor("initial prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			socket := filepath.Join(s.stateDir, "inner", "gtx.sock")
			stop := func() {
				if c, err := net.Dial("unix", socket); err == nil {
					_ = json.NewEncoder(c).Encode(map[string]any{"id": "stop-inner", "method": "server.stop", "params": map[string]any{}})
					c.Close()
				}
			}
			t.Cleanup(stop)
			s.send(fmt.Sprintf("%q --session inner server\r", binPath))
			if !allow {
				s.waitFor("nested refusal", 5*time.Second, func(text string) bool { return strings.Contains(text, "refusing to start a nested ikesu server") })
				if _, err := os.Stat(socket); !os.IsNotExist(err) {
					t.Fatal("refused server created socket", err)
				}
				return
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				c, err := net.Dial("unix", socket)
				if err == nil {
					c.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("allowed nested server did not start", err)
				}
				s.pump(time.Now().Add(50 * time.Millisecond))
			}
			stop()
			s.waitFor("nested server exit", 5*time.Second, func(text string) bool { return strings.Count(text, "❯") >= 2 })
		})
	}
}
