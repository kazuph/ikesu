package guard

import (
	"encoding/json"
	"fmt"
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
	b, e := os.ReadFile(a.MarkerPath)
	var marker Marker
	if e != nil || json.Unmarshal(b, &marker) != nil || !reflect.DeepEqual(marker, a.meta) || !reflect.DeepEqual(marker.Policy, &a.Config) || marker.Agent != a.active || marker.Mode != a.Config.EffectiveMode() {
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
	if reason == "" {
		a.issue = ""
		e := os.Remove(a.issuePath())
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	if a.issue == reason && regularFile(a.issuePath()) {
		return nil
	}
	a.issue = reason
	i := CoverageIssue{Pane: a.meta.Pane, Reason: reason, Recovery: "guard が有効な状態で新しい pane を開き、agent を起動し直してください"}
	b, e := json.Marshal(i)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(a.issuePath()), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(a.issuePath()), ".issue-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), a.issuePath()); e == nil {
		a.issue = reason
	}
	return e
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
