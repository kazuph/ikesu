package tui

import (
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
)

func TestMachineFormRetainsCursorEditingAndOutsideClick(t *testing.T) {
	a := specRuntimeApp(t)
	a.SetMachineCatalog(filepath.Join(t.TempDir(), "machines.json"))
	a.addMachineDialog()
	a.dialogKey(uv.Key{Text: "a界b"})
	a.dialogKey(uv.Key{Code: uv.KeyLeft})
	a.dialogKey(uv.Key{Code: uv.KeyBackspace})
	a.dialogKey(uv.Key{Text: "Z"})
	if a.dialog.d.Input != "aZb" || a.dialog.d.InputCursor != 2 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyDelete})
	a.dialogKey(uv.Key{Code: uv.KeyHome})
	a.dialogKey(uv.Key{Text: "X"})
	a.dialogKey(uv.Key{Code: uv.KeyEnd})
	a.dialogKey(uv.Key{Text: "Y"})
	if a.dialog.d.Input != "XaZY" {
		t.Fatal(a.dialog.d)
	}
	d := a.dialog
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
		a.onMouse(input.Event{Kind: kind, Mouse: uv.Mouse{X: 0, Y: 0, Button: uv.MouseLeft}})
		if a.dialog != d || a.dialog.d.Input != "XaZY" {
			t.Fatal("outside press/release dismissed or changed the form")
		}
	}
	a.dialogKey(uv.Key{Code: 'c', Mod: uv.ModCtrl})
	if a.dialog != d || a.dialog.d.Input != "" {
		t.Fatal("clear must keep the form")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || len(a.machines().Load()) != 0 {
		t.Fatal("escape must cancel without saving")
	}
}

func TestMachineMenuFormsCancelAndStaleRemoval(t *testing.T) {
	a := specRuntimeApp(t)
	a.SetMachineCatalog(filepath.Join(t.TempDir(), "machines.json"))
	a.openMainMenu(0, 0)
	index := -1
	remove := -1
	for i, it := range a.menu.m.Items {
		if it.Label == "add machine..." {
			index = i
		}
		if it.Label == "remove machine..." {
			remove = i
		}
	}
	if index < 0 || remove < 0 {
		t.Fatal("machine menu absent")
	}
	a.activateMenu(index)
	if a.dialog == nil || a.dialog.d.Title != "SSH target" {
		t.Fatal(a.dialog)
	}
	a.dialogKey(uv.Key{Text: "offline.invalid", Code: 'o'})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.dialog.d.Title != "Display name" {
		t.Fatal(a.dialog)
	}
	a.dialogKey(uv.Key{Text: "offline", Code: 'o'})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.dialog.d.Title != "Remote session" {
		t.Fatal(a.dialog)
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || len(a.machines().Load()) != 0 {
		t.Fatal("cancel saved")
	}
	c := a.machines()
	p, err := c.AddVerified("host", "one", "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.AddVerified("other", "two", "default")
	if err != nil {
		t.Fatal(err)
	}
	a.removeMachineMenu()
	if a.menu.kind != menuMachineRemove || len(a.menu.machines) != 2 {
		t.Fatal(a.menu)
	}
	a.activateMenu(0)
	if a.dialog.d.Title != "remove machine?" {
		t.Fatal(a.dialog)
	}
	c.Change(p.ID, "rename", "changed", nil)
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.dialog == nil || !strings.Contains(a.dialog.d.Detail, "changed") || len(c.Load()) != 2 {
		t.Fatal("stale removal accepted")
	}
	renamed, _ := machine.Resolve(c.Load(), p.ID, false)
	a.removeMachineDialog(renamed)
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if got := c.Load(); len(got) != 1 || got[0] != other {
		t.Fatal(got)
	}
}

func TestMachineProbeCancelIsNonblocking(t *testing.T) {
	a := specRuntimeApp(t)
	a.SetMachineCatalog(filepath.Join(t.TempDir(), "machines.json"))
	// Probe uses real ssh; the UI event loop must remain available even
	// while DNS or connection establishment is pending.
	t.Cleanup(func() { close(a.doneCh) })
	a.probeMachine("offline.invalid", "offline", "default")
	if a.dialog == nil || a.dialog.d.Title != "Checking SSH reachability" {
		t.Fatal(a.dialog)
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || len(a.machines().Load()) != 0 {
		t.Fatal("cancel saved")
	}
}
