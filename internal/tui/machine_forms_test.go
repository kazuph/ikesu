package tui

import (
	"context"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/render"
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
	if in.Menu == nil || len(in.Menu.Items) != 2 || in.Menu.Items[0].Label != "add machine..." || in.Menu.Items[1].Label != "remove machine..." || in.Menu.X != 2 || in.Menu.Y != 8 {
		t.Fatal(in.Menu)
	}
	for _, key := range []uv.Key{{Code: uv.KeyDown}, {Code: uv.KeyEnter}} {
		forms.Input(input.Event{Kind: input.KindKey, Key: key}, state)
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
