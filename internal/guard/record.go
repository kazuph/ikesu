package guard

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/childenv"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Record struct {
	Epoch       int64    `json:"ts_epoch"`
	Timestamp   string   `json:"ts"`
	Decision    string   `json:"verdict"`
	Rule        string   `json:"rule"`
	Reason      string   `json:"reason,omitempty"`
	Command     []string `json:"cmd"`
	CommandLine string   `json:"cmd_line"`
	Cwd         string   `json:"cwd"`
	LaunchCwd   string   `json:"launch_dir"`
	Pane        string   `json:"pane_id,omitempty"`
	Workspace   string   `json:"workspace_id,omitempty"`
	Tab         string   `json:"tab_id,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	Via         string   `json:"via"`
	Suppressed  string   `json:"suppressed_by,omitempty"`
}

func ReadMarker(i Input) (Marker, bool, error) {
	p := i.Env["GTX_GUARD_MARKER"]
	if p == "" {
		return Marker{}, false, nil
	}
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return Marker{}, false, nil
	}
	if e != nil {
		return Marker{}, false, e
	}
	var m Marker
	if e = json.Unmarshal(b, &m); e != nil {
		return m, false, e
	}
	if m.Agent == "" || m.Pane != i.Env["GTX_PANE_ID"] || m.Socket != i.Env["GTX_SOCKET_PATH"] {
		return m, false, nil
	}
	return m, true, nil
}
func Log(root string, i Input, m Marker, v Verdict, via string) error {
	d := filepath.Join(root, "guard")
	if e := os.MkdirAll(d, 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(d, "decisions.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		return e
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	now := time.Now()
	b, e := json.Marshal(Record{Epoch: now.Unix(), Timestamp: now.UTC().Format(time.RFC3339), Decision: v.Decision, Rule: v.Rule, Reason: v.Reason, Command: append([]string{i.Command}, i.Args...), CommandLine: i.Line(), Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Pane: m.Pane, Workspace: m.Workspace, Tab: m.Tab, Agent: m.Agent, Via: via, Suppressed: v.Suppressed})
	if e != nil {
		return e
	}
	b = append(b, '\n')
	_, e = f.Write(b)
	return e
}

func Execute(i Input) error {
	if e := os.Chdir(i.Cwd); e != nil {
		return e
	}
	env := childenv.Scrub(environment(i.Env))
	path, e := Resolve(i.Command, env)
	if e != nil {
		return e
	}
	return syscall.Exec(path, append([]string{i.Command}, i.Args...), env)
}
