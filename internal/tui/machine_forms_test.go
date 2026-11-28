package tui

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestClientMachineFormsRetainObservedDefaultsBackNavigationAndLocalCatalog(t *testing.T) {
	state := specRuntimeApp(t).st
	path := filepath.Join(t.TempDir(), "hand", "machines.json")
	catalog := machine.Catalog{Path: path}
	profile, err := catalog.AddVerified("saved-host", "Saved name", "remote")
	if err != nil {
		t.Fatal(err)
	}
	forms := NewMachineForms(context.Background(), path, nil)
	defer forms.Close()
	if err := forms.Open("add", state); err != nil {
		t.Fatal(err)
	}
	key := func(k uv.Key) {
		if !forms.Input(input.Event{Kind: input.KindKey, Key: k}, state) {
			t.Fatal("form lost input ownership")
		}
	}
	check := func(title, value string) {
		var in render.Input
		forms.Overlay(&in)
		if in.Dialog == nil || in.Dialog.Title != title || in.Dialog.Input != value || !in.Dialog.PreserveTitleCase {
			t.Fatal(in.Dialog)
		}
	}
	check("add machine: SSH target (e.g. mini)", "")
	key(uv.Key{Text: "own-host"})
	key(uv.Key{Code: uv.KeyEnter})
	check("add machine: display name (shift-tab back)", "own-host")
	key(uv.Key{Code: 'c', Mod: uv.ModCtrl})
	key(uv.Key{Text: "Own name"})
	key(uv.Key{Code: uv.KeyEnter})
	check("add machine: remote session (shift-tab back)", "default")
	key(uv.Key{Code: uv.KeyTab, Mod: uv.ModShift})
	check("add machine: display name (shift-tab back)", "Own name")
	key(uv.Key{Code: uv.KeyTab, Mod: uv.ModShift})
	check("add machine: SSH target (e.g. mini)", "own-host")
	key(uv.Key{Code: uv.KeyEscape})
	if got := catalog.Load(); len(got) != 1 || got[0] != profile {
		t.Fatal("cancel changed the catalog", got)
	}
	if err := forms.Open("remove", state); err != nil {
		t.Fatal(err)
	}
	var in render.Input
	forms.Overlay(&in)
	if in.Menu == nil || len(in.Menu.Items) != 1 || in.Menu.Items[0].Label != "Saved name" || in.Menu.Sel != 0 || in.Menu.X != 0 || in.Menu.Y != state.Rows {
		t.Fatal(in.Menu)
	}
	key(uv.Key{Code: uv.KeyEscape})
	if forms.Input(input.Event{Kind: input.KindKey, Key: uv.Key{Text: "x"}}, state) {
		t.Fatal("cancel retained input ownership")
	}
}

func TestClientMachineHeaderRemovalRejectsChangedProfileAndKeepsOtherMachine(t *testing.T) {
	state := specRuntimeApp(t).st
	catalog := machine.Catalog{Path: filepath.Join(t.TempDir(), "hand", "machines.json")}
	selected, err := catalog.AddVerified("selected-host", "Selected", "remote")
	if err != nil {
		t.Fatal(err)
	}
	other, err := catalog.AddVerified("other-host", "Other", "other-session")
	if err != nil {
		t.Fatal(err)
	}
	forms := NewMachineForms(context.Background(), catalog.Path, nil)
	defer forms.Close()
	forms.OpenMachine(selected, state, 2, 8)
	var in render.Input
	forms.Overlay(&in)
	if in.Menu == nil || in.Menu.X != 2 || in.Menu.Y != 8 {
		t.Fatal(in.Menu)
	}
	for _, code := range []rune{uv.KeyDown, uv.KeyDown, uv.KeyEnter} {
		forms.Input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: code}}, state)
	}
	in = render.Input{}
	forms.Overlay(&in)
	if in.Dialog == nil || in.Dialog.Title != "remove machine?" || in.Dialog.Detail != "Selected — remote sessions keep running" || in.Dialog.Confirm != "confirm" || !in.Dialog.CenterTerminal || !in.Dialog.CompactButtons {
		t.Fatal(in.Dialog)
	}
	if _, err := catalog.Change(selected.ID, "rename", "Renamed", nil); err != nil {
		t.Fatal(err)
	}
	forms.Input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}, state)
	in = render.Input{}
	forms.Overlay(&in)
	if in.Dialog == nil || in.Dialog.Detail != "saved machine changed; reopen the machine list" {
		t.Fatal(in.Dialog)
	}
	got := catalog.Load()
	if len(got) != 2 || got[0].ID != selected.ID || got[0].Label != "Renamed" || got[1] != other {
		t.Fatal("stale header deletion changed the catalog", got)
	}
}

func TestClientMachineHeaderConnectionMenu(t *testing.T) {
	state := specRuntimeApp(t).st
	catalog := machine.Catalog{Path: filepath.Join(t.TempDir(), "machines.json")}
	profile, err := catalog.AddVerified("host", "Remote", "default")
	if err != nil {
		t.Fatal(err)
	}
	forms := NewMachineForms(context.Background(), catalog.Path, nil)
	defer forms.Close()
	for _, item := range []string{"disconnect", "connect"} {
		forms.OpenMachine(profile, state, 2, 8)
		var in render.Input
		forms.Overlay(&in)
		want := []render.MenuItem{{Label: item}, {Separator: true}, {Label: "add machine..."}, {Label: "remove machine..."}}
		if in.Menu == nil || in.Menu.Sel != 0 || !slices.Equal(in.Menu.Items, want) {
			t.Fatalf("expected %+v with first item selected, got %+v", want, in.Menu)
		}
		forms.Input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: uv.KeyEnter}}, state)
		saved := catalog.Load()
		if len(saved) != 1 || saved[0].ID != profile.ID || saved[0].Enabled == profile.Enabled {
			t.Fatalf("%s did not update saved machine: %+v", item, saved)
		}
		profile = saved[0]
	}
	forms.OpenMachine(machine.Profile{ID: "local", Enabled: true}, state, 2, 8)
	var in render.Input
	forms.Overlay(&in)
	want := []render.MenuItem{{Label: "add machine..."}, {Label: "remove machine..."}}
	if in.Menu == nil || in.Menu.Sel != 0 || !slices.Equal(in.Menu.Items, want) {
		t.Fatalf("expected Local menu %+v with first item selected, got %+v", want, in.Menu)
	}
}

func TestClientMachineHeaderSeparatorMatchesPaneMenu(t *testing.T) {
	t.Parallel()
	state := model.New(t.TempDir(), 100, 30)
	forms := NewMachineForms(context.Background(), "", nil)
	defer forms.Close()
	forms.app.st = state
	forms.app.openPaneMenu(45, 5, int(state.FocusedPane()))
	in := render.Input{State: state, Menu: &forms.app.menu.m}
	in.SidebarConfig.Theme.Name = "tokyo-night"
	local := render.Input{State: state, SidebarConfig: in.SidebarConfig}
	sidebar := &render.MachineSidebar{Active: "local", Groups: []render.MachineGroup{
		{ID: "local", Label: "Local", Status: "online", Input: &local},
		{ID: "remote", Label: "Remote", Status: "disconnected"},
	}}
	pane := render.ComposeClient(in, sidebar, "", false)
	paneRule := pane.MenuItemRows[2]
	for _, enabled := range []bool{false, true} {
		forms.OpenMachine(machine.Profile{ID: "remote", Enabled: enabled}, state, 2, 8)
		forms.Overlay(&in)
		machine := render.ComposeClient(in, sidebar, "", false)
		rule := machine.MenuItemRows[1]
		for dx := 2; dx < 4; dx++ {
			want := pane.Frame.Cell(paneRule.X+dx, paneRule.Y)
			got := machine.Frame.Cell(rule.X+dx, rule.Y)
			if want.Content != "-" || got.Content != want.Content || got.Style.Fg != want.Style.Fg || got.Style.Bg != want.Style.Bg || got.Style.Fg == got.Style.Bg {
				t.Fatalf("enabled=%t separator cell %d: got %q (%v on %v), want pane menu hyphen %q (%v on %v)", enabled, dx, got.Content, got.Style.Fg, got.Style.Bg, want.Content, want.Style.Fg, want.Style.Bg)
			}
		}
	}
}

func TestClientMachineHeaderSeparator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := model.New(dir, 24, 20)
	state.MobileWidthThreshold = 64
	catalog := machine.Catalog{Path: filepath.Join(dir, "machines.json")}
	profile, err := catalog.AddVerified("host", "Remote", "default")
	if err != nil {
		t.Fatal(err)
	}
	forms := NewMachineForms(context.Background(), catalog.Path, nil)
	defer forms.Close()
	anchor := uv.Mouse{X: 2, Y: 8, Button: uv.MouseRight}
	forms.OpenMachine(profile, state, anchor.X, anchor.Y)
	in := render.Input{State: state}
	forms.Overlay(&in)
	var signal render.NativeMenuSignal
	native := nativeSignals(t, signal.Next(true, in.Menu, in.Dialog))
	if len(native) != 1 || native[0]["sel"] != float64(0) {
		t.Fatalf("native menu selection = %+v", native)
	}
	items := native[0]["items"].([]any)
	if len(items) != 4 || items[1].(map[string]any)["separator"] != true {
		t.Fatalf("native menu items = %+v", items)
	}
	out := render.Compose(in)
	row := out.MenuItemRows[1]
	for dx := 2; dx < 4; dx++ {
		if got := out.Frame.Cell(row.X+dx, row.Y).Content; got != "-" {
			t.Fatalf("mobile menu separator cell %d = %q, want hyphen", dx, got)
		}
	}
	for _, step := range []struct {
		code rune
		sel  int
	}{{uv.KeyDown, 2}, {uv.KeyUp, 0}} {
		forms.Input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: step.code}}, state)
		forms.Overlay(&in)
		if in.Menu.Sel != step.sel {
			t.Fatalf("selection after %v = %d, want %d", step.code, in.Menu.Sel, step.sel)
		}
	}
	forms.Input(input.Event{Kind: input.KindMouseRelease, Mouse: anchor}, state)
	mouse := uv.Mouse{X: row.X, Y: row.Y, Button: uv.MouseLeft}
	forms.Input(input.Event{Kind: input.KindMouseMotion, Mouse: mouse}, state)
	forms.Overlay(&in)
	if in.Menu.Sel != 0 || in.Menu.Hover != -1 {
		t.Fatalf("hovering the separator changed selection: %+v", in.Menu)
	}
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
		if !forms.Input(input.Event{Kind: kind, Mouse: mouse}, state) {
			t.Fatalf("menu did not consume the separator click event %v", kind)
		}
	}
	in = render.Input{}
	forms.Overlay(&in)
	if saved := catalog.Load(); in.Dialog != nil || !slices.Equal(saved, []machine.Profile{profile}) {
		t.Fatalf("clicking the separator triggered an action: dialog=%+v catalog=%+v", in.Dialog, saved)
	}
}
