package guard

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type CoverageIssue struct {
	Pane     string `json:"pane_id"`
	Reason   string `json:"reason"`
	Recovery string `json:"recovery"`
}

func (i CoverageIssue) String() string {
	return fmt.Sprintf("見張り未適用 %s: %s。%s", i.Pane, i.Reason, i.Recovery)
}
func (a *Assets) issuePath() string {
	return filepath.Join(a.Root, "guard", "uncovered", a.Pane+".json")
}
func (a *Assets) coverageReason() string {
	if !a.injected {
		return "起動時の環境に印と shim の PATH がありません"
	}
	if a.unsupported != "" {
		return a.unsupported
	}
	if a.mode != a.Config.EffectiveMode() {
		return "起動時の guard モードと再読込後のモードが異なります"
	}
	if a.prompt && !regularFile(filepath.Join(a.Root, "guard", "shell", a.Pane, "ready")) {
		return "bash の起動後に shim を先頭へ戻す処理が実行されていません"
	}
	return ""
}
func (a *Assets) intact() bool {
	if !a.runtimeIntact() {
		return false
	}
	b, e := os.ReadFile(a.MarkerPath)
	marker, decodeErr := decodeMarker(b)
	if e != nil || decodeErr != nil || !reflect.DeepEqual(marker, a.meta) || !reflect.DeepEqual(marker.Policy, &a.Config) || marker.Agent != a.active || marker.Mode != a.Config.EffectiveMode() {
		return false
	}
	for _, cmd := range a.Config.Commands() {
		f, e := os.Stat(filepath.Join(a.Bin, cmd))
		if e != nil || !f.Mode().IsRegular() || f.Mode()&0100 == 0 {
			return false
		}
	}
	return true
}
func (a *Assets) setIssue(reason string) error {
	if err := a.validateManaged(false); err != nil {
		if reason != "" {
			a.issue = reason
			return err
		}
		if !os.IsNotExist(err) {
			a.issue = err.Error()
			return err
		}
	}
	a.issue = reason
	dir, err := a.privateIssue(reason != "")
	if err != nil {
		if reason == "" && os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer dir.close()
	if reason == "" {
		err := unix.Unlinkat(dir.fd(), filepath.Base(a.issuePath()), 0)
		if err == unix.ENOENT {
			return nil
		}
		return err
	}
	issue := CoverageIssue{Pane: a.meta.Pane, Reason: reason, Recovery: "guard が有効な状態で新しい pane を開き、agent を起動し直してください"}
	data, err := json.Marshal(issue)
	if err != nil {
		return err
	}
	return dir.write(filepath.Base(a.issuePath()), data, 0600)
}
func (a *Assets) clearCoverage() error {
	if e := a.remove(); e != nil {
		return e
	}
	return a.setIssue("")
}
func (a *Assets) Reconfigure(c Config) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	defer a.refreshChanges()
	// Drop forwarding entries for targets removed by the new configuration.
	for _, cmd := range a.Config.Commands() {
		if !has(c.Commands(), cmd) {
			if e := os.Remove(filepath.Join(a.Bin, cmd)); e != nil && !os.IsNotExist(e) {
				return a.setIssue(e.Error())
			}
		}
	}
	a.Config = c
	return a.sync(a.active)
}
func (a *Assets) Issue() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.issue == "" {
		return ""
	}
	return (CoverageIssue{a.meta.Pane, a.issue, "新しい pane で agent を起動し直してください"}).String()
}
func CoverageIssues(root string) ([]CoverageIssue, error) {
	dir := filepath.Join(root, "guard", "uncovered")
	files, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var issues []CoverageIssue
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, file.Name()))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var issue CoverageIssue
		if e = json.Unmarshal(b, &issue); e != nil {
			return nil, e
		}
		issues = append(issues, issue)
	}
	return issues, nil
}
