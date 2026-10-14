package herdrimport

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "herdr-import-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	src := map[string]any{"version": 5, "active": 0, "selected": 0, "sidebar_width": 27, "workspaces": []any{map[string]any{"id": "s1", "custom_name": "work", "section": "work", "identity_cwd": d, "public_tab_numbers": []int{1}, "next_public_tab_number": 2, "active_tab": 0, "tabs": []any{map[string]any{"custom_name": "dev", "layout": map[string]any{"Split": map[string]any{"direction": "Horizontal", "ratio": 0.3, "first": map[string]int{"Pane": 7}, "second": map[string]int{"Pane": 8}}}, "pane_order": []int{8, 7}, "focused": 8, "root_pane": 7, "panes": map[string]any{"7": map[string]any{"global_pane_number": 7, "cwd": d, "agent_session": map[string]string{"agent": "claude", "kind": "id", "value": "exact-alpha"}}, "8": map[string]any{"global_pane_number": 8, "cwd": d, "managed_agent_kind": "claude", "label": "missing"}}}}}}}
	b, _ := json.Marshal(src)
	if err := os.WriteFile(filepath.Join(d, "session.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return d
}
func change(t *testing.T, d string, fn func(map[string]any)) {
	t.Helper()
	path := filepath.Join(d, "session.json")
	b, _ := os.ReadFile(path)
	var src map[string]any
	json.Unmarshal(b, &src)
	fn(src)
	b, _ = json.Marshal(src)
	os.WriteFile(path, b, 0600)
}
func tabMap(src map[string]any) map[string]any {
	return src["workspaces"].([]any)[0].(map[string]any)["tabs"].([]any)[0].(map[string]any)
}
func TestConversionPreservesStructureAndExactID(t *testing.T) {
	d := fixture(t)
	p, err := Load(d, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	w := p.File.Workspaces[0]
	tab := w.Tabs[0]
	if w.Name != "work" || w.Section != "work" || w.ActiveTab != 0 || tab.Name != "dev" || tab.Focused != 8 || tab.Order[0] != 8 || tab.Layout.Direction != "h" || tab.Layout.Ratio != 0.3 {
		t.Fatalf("wrong conversion: %+v %+v", w, tab)
	}
	if tab.Panes[1].SessionID != "exact-alpha" || !strings.Contains(p.Panes[0].Reason, "会話 ID 不明") {
		t.Fatalf("wrong refs: %+v", p.Panes)
	}
	if p.File.SidebarWidth != 27 || p.File.Active != 0 {
		t.Fatal("wrong sidebar/focus")
	}
}
func TestRejectUnreadableAndUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"version", func(s map[string]any) { s["version"] = 4 }},
		{"unknown", func(s map[string]any) { s["surprise"] = true }},
		{"focus", func(s map[string]any) { tabMap(s)["focused"] = 99 }},

		{"direction", func(s map[string]any) {
			tabMap(s)["layout"].(map[string]any)["Split"].(map[string]any)["direction"] = "Diagonal"
		}},
		{"ratio", func(s map[string]any) { tabMap(s)["layout"].(map[string]any)["Split"].(map[string]any)["ratio"] = 1 }},
		{"missingcwd", func(s map[string]any) {
			tabMap(s)["panes"].(map[string]any)["7"].(map[string]any)["cwd"] = "/no-such-import-dir"
		}},
		{"duplicate", func(s map[string]any) { tabMap(s)["pane_order"] = []int{7, 7} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := fixture(t)
			change(t, d, tc.edit)
			if _, err := Load(d, config.Config{}); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
	d := fixture(t)
	os.WriteFile(filepath.Join(d, "session.json"), []byte("{"), 0600)
	if _, err := Load(d, config.Config{}); err == nil {
		t.Fatal("accepted corrupt json")
	}
}
func TestRunningSocketAndLockSuppressAllAgents(t *testing.T) {
	for _, mode := range []string{"socket", "lock"} {
		t.Run(mode, func(t *testing.T) {
			d := fixture(t)
			if mode == "socket" {
				l, err := net.Listen("unix", filepath.Join(d, "herdr.sock"))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			} else {
				f, err := os.Create(filepath.Join(d, "herdr.lock"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			p, err := Load(d, config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if !p.Running {
				t.Fatal("missed live herdr")
			}
			for _, x := range p.File.Workspaces[0].Tabs[0].Panes {
				if x.SessionID != "" || x.Agent != "" {
					t.Fatal("live agent retained", x)
				}
			}
		})
	}
}
func TestExactSupplementAndConflict(t *testing.T) {
	d := fixture(t)
	write := func(id string) {
		b, _ := json.Marshal(ledger{Entries: map[string]ledgerRow{"row": {Pane: 8, Workspace: "s1", Tab: "s1:t1", Cwd: d, Agent: "claude", ID: id}}})
		os.WriteFile(filepath.Join(d, "agent-session-ledger.json"), b, 0600)
	}
	write("exact-beta")
	p, err := Load(d, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if p.File.Workspaces[0].Tabs[0].Panes[0].SessionID != "exact-beta" {
		t.Fatal("exact ledger not used")
	}
	b, _ := json.Marshal(snapshot{Agents: []snapshotRow{{Pane: "p8", Workspace: "s1", Cwd: d, Agent: "claude", ID: "other"}}})
	os.WriteFile(filepath.Join(d, "agent-session-snapshot.json"), b, 0600)
	p, err = Load(d, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if p.File.Workspaces[0].Tabs[0].Panes[0].SessionID != "" || !strings.Contains(p.Panes[0].Reason, "矛盾") {
		t.Fatal("conflicting exact refs resumed")
	}
}
func TestUnsafeAndDuplicateReferencesStayShell(t *testing.T) {
	for _, id := range []string{"--last", "exact-alpha"} {
		t.Run(id, func(t *testing.T) {
			d := fixture(t)
			change(t, d, func(src map[string]any) {
				tabMap(src)["panes"].(map[string]any)["8"].(map[string]any)["agent_session"] = map[string]string{"agent": "claude", "kind": "id", "value": id}
			})
			p, err := Load(d, config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if p.File.Workspaces[0].Tabs[0].Panes[0].SessionID != "" {
				t.Fatal("unsafe/duplicate resumed")
			}
		})
	}
}

func TestHerdrZoomRestoresFocusedPane(t *testing.T) {
	d := fixture(t)
	change(t, d, func(s map[string]any) { tabMap(s)["zoomed"] = true })
	p, err := Load(d, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tab := p.File.Workspaces[0].Tabs[0]
	if tab.Zoomed != 8 || tab.Focused != 8 {
		t.Fatalf("zoom changed pane: %+v", tab)
	}
}

func TestMultipleWorkspaceTabsRetainIndependentFocus(t *testing.T) {
	d := fixture(t)
	change(t, d, func(src map[string]any) {
		w := src["workspaces"].([]any)[0].(map[string]any)
		clone := func(value any) map[string]any {
			b, _ := json.Marshal(value)
			var out map[string]any
			json.Unmarshal(b, &out)
			return out
		}
		tab2 := clone(tabMap(src))
		tab2["custom_name"] = "second"
		tab2["layout"] = map[string]int{"Pane": 9}
		tab2["pane_order"] = []int{9}
		tab2["focused"] = 9
		tab2["root_pane"] = 9
		tab2["panes"] = map[string]any{"9": map[string]any{"global_pane_number": 9, "cwd": d}}
		w["tabs"] = append(w["tabs"].([]any), tab2)
		w["active_tab"] = 1
		w["public_tab_numbers"] = []int{1, 2}
		w["next_public_tab_number"] = 3
		w2 := clone(w)
		w2["id"] = "s2"
		w2["custom_name"] = "second space"
		w2["active_tab"] = 0
		w2["tabs"] = []any{map[string]any{"custom_name": "third", "layout": map[string]int{"Pane": 10}, "pane_order": []int{10}, "focused": 10, "root_pane": 10, "panes": map[string]any{"10": map[string]any{"global_pane_number": 10, "cwd": d}}}}
		w2["public_tab_numbers"] = []int{1}
		w2["next_public_tab_number"] = 2
		src["workspaces"] = []any{w, w2}
		src["active"] = 1
		src["selected"] = 1
	})
	p, err := Load(d, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if p.File.Active != 1 || p.File.Workspaces[0].ActiveTab != 1 || len(p.File.Workspaces[0].Tabs) != 2 || p.File.Workspaces[1].Tabs[0].Focused != 10 || p.File.Workspaces[1].Name != "second space" {
		t.Fatalf("independent focus lost: %+v", p.File)
	}
}
