package tui

import (
	"context"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"sync"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// MachineForms shares the native form controls without owning a model or PTYs.
// The caller serializes post callbacks with input and closes its parent context.
type MachineForms struct {
	app    *App
	cancel context.CancelFunc
}

func NewMachineForms(ctx context.Context, catalog string, post func(func())) *MachineForms {
	ctx, cancel := context.WithCancel(ctx)
	return &MachineForms{app: &App{machineCatalog: catalog, machineContext: ctx, machinePost: post, machineWorkers: &sync.WaitGroup{}, clientMachines: true}, cancel: cancel}
}

func (f *MachineForms) Open(action string, state *model.AppState) error {
	f.app.st = state
	switch action {
	case "add":
		f.app.addMachineDialog()
	case "remove":
		f.app.removeMachineMenu()
	case "menu":
		f.app.openMainMenu(0, render.SidebarMenuRect(state).Y)
	default:
		return fmt.Errorf("endpoint_action_unsupported: machine menu %q", action)
	}
	return nil
}

func (f *MachineForms) Input(event input.Event, state *model.AppState) bool {
	a := f.app
	a.st = state
	if a.dialog == nil && a.menu == nil {
		return false
	}
	switch event.Kind {
	case input.KindKey:
		if a.dialog != nil {
			a.dialogKey(event.Key)
		} else {
			a.menuKey(event.Key)
		}
	case input.KindPaste:
		if a.dialog != nil && a.dialog.input != nil {
			a.machineInputKey(uv.Key{Text: event.Text})
		}
	case input.KindMouseClick, input.KindMouseRelease, input.KindMouseMotion, input.KindMouseWheel:
		if a.menu != nil && event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseRight && !pointIn(event.Mouse.X, event.Mouse.Y, render.MenuRect(&a.menu.m, state.Cols, state.Rows)) {
			a.menu = nil
			return false
		}
		if a.dialog != nil {
			a.dialogMouse(event)
		} else {
			a.menuMouse(event)
		}
	default:
		return false
	}
	return true
}

func (f *MachineForms) OpenMachine(profile machine.Profile, state *model.AppState, x, y int) {
	f.CloseDialog()
	f.app.st = state
	f.app.menu = &uiMenu{kind: menuMachineManage, machines: []machine.Profile{profile}, justOpened: true, m: render.Menu{X: x, Y: y, Global: true, Hover: -1, Sel: 0, Items: []render.MenuItem{{Label: "add machine..."}, {Label: "remove machine..."}}}}
}

func (f *MachineForms) CloseDialog() {
	if f.app.dialog != nil {
		f.app.cancelDialog()
	}
}

func (f *MachineForms) Overlay(in *render.Input) {
	if f.app.dialog != nil {
		d := f.app.dialog.d
		in.Dialog, in.Menu, in.Help, in.Settings = &d, nil, nil, nil
	}
	if f.app.menu != nil {
		m := f.app.menu.m
		in.Menu, in.Dialog, in.Help, in.Settings = &m, nil, nil, nil
	}
}

func (f *MachineForms) Close() {
	f.cancel()
	if f.app.dialog != nil {
		f.app.cancelDialog()
	}
	f.app.menu = nil
	f.app.machineWorkers.Wait()
}

func (v *Viewer) SetMachineRequester(request func(string) error) { v.app.machineRequest = request }

func (a *App) clientMachineStep(step int, target, label, session string) {
	values := []string{target, label, session}
	titles := []string{"add machine: SSH target (e.g. mini)", "add machine: display name (shift-tab back)", "add machine: remote session (shift-tab back)"}
	a.machineInput(titles[step], values[step], func(value string) {
		values[step] = value
		if step == 0 && values[1] == "" {
			values[1] = value
		}
		if step == 2 {
			a.probeMachine(values[0], values[1], values[2])
		} else {
			a.clientMachineStep(step+1, values[0], values[1], values[2])
		}
	})
	a.dialog.d.PreserveTitleCase = true
	if step > 0 {
		d := a.dialog
		d.back = func() { values[step] = *d.input; a.clientMachineStep(step-1, values[0], values[1], values[2]) }
	}
}
