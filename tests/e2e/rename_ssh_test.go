//go:build stage3_ssh

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/machine"
)

// This uses an actual unprivileged OpenSSH server, ephemeral keys and homes.
// The SSH launcher only selects its isolated config; it executes real ssh.
func TestStageThreeSSHVersionMatrix(t *testing.T) {
	baseline := prepareMigrationBaseline(t)
	for _, pair := range []struct{ name, local, remote, remoteName string }{
		{"new-new", binPath, binPath, "ikesu"},
		{"new-old", binPath, baseline, "gtx"},
		{"old-new", baseline, binPath, "ikesu"},
	} {
		t.Run(pair.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "is-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			remoteHome := filepath.Join(root, "remote")
			localHome := filepath.Join(root, "local")
			binDir := filepath.Join(root, "bin")
			for _, path := range []string{remoteHome, localHome, binDir} {
				if err = os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			remoteBin := filepath.Join(binDir, pair.remoteName)
			raw, err := os.ReadFile(pair.remote)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(remoteBin, raw, 0700); err != nil {
				t.Fatal(err)
			}
			remoteConfig := filepath.Join(remoteHome, ".config", pair.remoteName)
			if err = os.MkdirAll(remoteConfig, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(remoteConfig, "config.toml"), []byte("onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			socketName := "ikesu.sock"
			if pair.remoteName == "gtx" {
				socketName = "gtx.sock"
			}
			remoteSocket := filepath.Join(remoteHome, ".local/state", pair.remoteName, "stage3", socketName)
			stopRemote := migrationServer(t, remoteBin, remoteHome, remoteSocket)
			defer stopRemote()
			for _, key := range []string{"host", "client"} {
				cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(root, key))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			current, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			force := filepath.Join(root, "remote-command")
			script := fmt.Sprintf("#!/bin/sh\nexport HOME=%s XDG_CONFIG_HOME=%s SHELL=/bin/sh PATH=%s:/usr/bin:/bin\nexec /bin/sh -c \"$SSH_ORIGINAL_COMMAND\"\n", machine.Quote(remoteHome), machine.Quote(filepath.Join(remoteHome, ".config")), machine.Quote(binDir))
			if err = os.WriteFile(force, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			sshdConfig := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s/host\nPidFile %s/sshd.pid\nAuthorizedKeysFile %s/client.pub\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers %s\nForceCommand %s\nLogLevel ERROR\n", port, root, root, root, current.Username, force)
			configPath := filepath.Join(root, "sshd.conf")
			if err = os.WriteFile(configPath, []byte(sshdConfig), 0600); err != nil {
				t.Fatal(err)
			}
			serverLog, err := os.Create(filepath.Join(root, "sshd.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer serverLog.Close()
			sshd := exec.Command("/usr/sbin/sshd", "-D", "-e", "-f", configPath)
			sshd.Stderr = serverLog
			if err = sshd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sshd.Process.Kill(); _ = sshd.Wait() }()
			deadline := time.Now().Add(15 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
					_ = c.Close()
					ready = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !ready {
				log, _ := os.ReadFile(filepath.Join(root, "sshd.log"))
				t.Fatal("sshd unavailable", string(log))
			}
			public, err := os.ReadFile(filepath.Join(root, "host.pub"))
			if err != nil {
				t.Fatal(err)
			}
			known := filepath.Join(root, "known_hosts")
			if err = os.WriteFile(known, []byte(fmt.Sprintf("[127.0.0.1]:%d %s", port, public)), 0600); err != nil {
				t.Fatal(err)
			}
			sshConfig := filepath.Join(root, "ssh.conf")
			settings := fmt.Sprintf("Host stage3-fixture\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s/client\n IdentitiesOnly yes\n UserKnownHostsFile %s\n StrictHostKeyChecking yes\n", port, current.Username, root, known)
			if err = os.WriteFile(sshConfig, []byte(settings), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Join(localHome, ".ssh"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(localHome, ".ssh/config"), []byte(settings), 0600); err != nil {
				t.Fatal(err)
			}
			launcher := filepath.Join(root, "launcher")
			if err = os.Mkdir(launcher, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(launcher, "ssh"), []byte("#!/bin/sh\nexec /usr/bin/ssh -F "+machine.Quote(sshConfig)+" \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			// Explicit paths are existing deployment settings, needed for mixed versions.
			env := append(migrationEnv(localHome), "PATH="+launcher+":"+os.Getenv("PATH"), "IKESU_MACHINE_REMOTE_BINARY="+remoteBin, "GTX_MACHINE_REMOTE_BINARY="+remoteBin)
			invoke := func(args ...string) []byte {
				t.Helper()
				cmd := exec.Command(pair.local, append([]string{"--session", "stage3"}, args...)...)
				cmd.Env = env
				out, err := cmd.CombinedOutput()
				if err != nil {
					log, _ := os.ReadFile(filepath.Join(root, "sshd.log"))
					t.Fatalf("%v: %v\n%s\nsshd:%s", args, err, out, log)
				}
				return out
			}
			invoke("machine", "add", "stage3-fixture", "--label", "remote", "--remote-session", "stage3")
			output := invoke("--machine", "remote", "pane", "list")
			if !strings.Contains(string(output), "pane_id") {
				t.Fatal(string(output))
			}
			var decision api.DecisionResult
			migrationRPC(t, remoteSocket, "decision.create", api.DecisionCreateParams{Kind: "ask", Title: "ssh-proof", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &decision)
			invoke("decision", "answer", decision.Decision.DecisionID, "yes", "--machine", "remote", "--responder", "ssh-test")
			var answered api.DecisionResult
			migrationRPC(t, remoteSocket, "decision.get", api.DecisionTargetParams{DecisionID: decision.Decision.DecisionID}, &answered)
			if answered.Decision.Status != "answered" || answered.Decision.Answer == nil || answered.Decision.Answer.Responder != "ssh-test" {
				t.Fatal(answered)
			}
			// Forward the ordinary run API over the real SSH endpoint and inspect its log.
			var panes struct {
				Panes []struct {
					PaneID string `json:"pane_id"`
				} `json:"panes"`
			}
			migrationRPC(t, remoteSocket, "pane.list", map[string]any{}, &panes)
			if len(panes.Panes) == 0 {
				t.Fatal("no remote panes")
			}
			// run --machine executes a remote command through the SSH transport; use
			// a local session for its job ledger and caller, as the normal CLI requires.
			localNS := "ikesu"
			if pair.local == baseline {
				localNS = "gtx-2e01183"
			}
			localConfig := filepath.Join(localHome, ".config", localNS)
			if err = os.MkdirAll(localConfig, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(localConfig, "config.toml"), []byte("onboarding=false\n[experimental]\nallow_nested=true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			localSocketName := "ikesu.sock"
			if pair.local == baseline {
				localSocketName = "gtx.sock"
			}
			localSocket := filepath.Join(localHome, ".local/state", localNS, "stage3", localSocketName)
			// The launcher is inherited by the local server's real SSH child job.
			originalPath := os.Getenv("PATH")
			t.Setenv("PATH", launcher+":"+originalPath)
			stopLocal := migrationServer(t, pair.local, localHome, localSocket)
			defer stopLocal()
			var localPanes struct {
				Panes []struct {
					PaneID string `json:"pane_id"`
				} `json:"panes"`
			}
			migrationRPC(t, localSocket, "pane.list", map[string]any{}, &localPanes)
			jobOut := invoke("run", "--machine", "remote", "--cwd", remoteHome, "--caller", localPanes.Panes[0].PaneID, "--completion", "none", "--", "/bin/sh", "-c", "printf ssh-run-proof")
			var started struct {
				Job string `json:"job"`
			}
			if err = json.Unmarshal(jobOut, &started); err != nil || started.Job == "" {
				t.Fatal(string(jobOut), err)
			}
			deadline = time.Now().Add(15 * time.Second)
			found := false
			for time.Now().Before(deadline) {
				out := invoke("log", started.Job)
				if strings.Contains(string(out), "ssh-run-proof") && strings.Contains(string(out), "exit_code: 0") {
					found = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !found {
				t.Fatal("remote run did not complete")
			}
			t.Logf("PASS %s: real SSH connection, remote run, decision answer; remote binary=%s", pair.name, remoteBin)
		})
	}
}
