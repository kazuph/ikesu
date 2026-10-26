package guard

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
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
	p := i.Env["IKESU_GUARD_MARKER"]
	if p == "" {
		return Marker{}, false, nil
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return Marker{Pane: i.Env["IKESU_PANE_ID"]}, true, fmt.Errorf("目印ファイルを読めません: %w", e)
	}
	m, e := decodeMarker(b)
	if e != nil {
		return m, true, fmt.Errorf("目印ファイルが壊れています: %w", e)
	}
	if m.Agent == "" || m.Pane != i.Env["IKESU_PANE_ID"] || !filepath.IsAbs(m.Socket) || !filepath.IsAbs(m.Cwd) {
		return m, true, fmt.Errorf("目印ファイルの画面・エージェント・接続先が不正です")
	}
	if m.Policy == nil {
		return m, true, fmt.Errorf("目印にサーバーのガード設定がありません")
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

// The PID distinguishes an exec loop from a real child process. The argv
// distinguishes direct reentry from an exec into another guarded command.
const shimExecContext = "IKESU_GUARD_EXEC_CONTEXT"

func executionContext(i Input) string {
	data, _ := json.Marshal(struct {
		PID  int
		Argv []string
	}{os.Getpid(), append([]string{i.Command}, i.Args...)})
	return string(data)
}

func CheckShimRecursion(i Input) error {
	if i.Env[shimExecContext] == executionContext(i) {
		return fmt.Errorf("身代わりスクリプトが自分自身を呼び直しました: %s", i.Command)
	}
	return nil
}

func Execute(i Input) error {
	if err := CheckShimRecursion(i); err != nil {
		return err
	}
	context := executionContext(i)
	if e := os.Chdir(i.Cwd); e != nil {
		return e
	}
	inherited := environment(i.Env)
	path, e := Resolve(i.Command, inherited)
	if e != nil {
		return e
	}
	clean := childenv.Scrub(inherited)
	// Scrub removes inherited guard state; this fresh process-local marker must
	// survive the exec so direct reentry cannot start a new guard invocation.
	clean = append(clean, shimExecContext+"="+context)
	return syscall.Exec(path, append([]string{i.Command}, i.Args...), clean)
}

// The server policy is storage-only; guard status retains its public marker shape.
func decodeMarker(b []byte) (Marker, error) {
	var stored struct {
		Marker
		Policy *Config `json:"guard_config"`
	}
	err := json.Unmarshal(b, &stored)
	stored.Marker.Policy = stored.Policy
	return stored.Marker, err
}
