package tui

import (
	"context"
	"fmt"
	"path/filepath"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) SetMachineCatalog(path string) { a.machineCatalog = path }
func (a *App) machines() machine.Catalog {
	path := a.machineCatalog
	if path == "" {
		path = filepath.Join(api.SessionDir(api.DefaultSession), "machines.json")
	}
	return machine.Catalog{Path: path}
}

func (a *App) machineError(err error) {
	a.dialog = &uiDialog{d: render.Dialog{Title: "Machine operation failed", Detail: err.Error(), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
	a.dirty = true
}

func (a *App) machineInput(title, value string, next func(string)) {
	a.dialog = &uiDialog{machineForm: true, input: &value, d: render.Dialog{Title: title, Input: value, InputCursor: len([]rune(value)), InputEdit: true, Confirm: "next", Cancel: "cancel"}, run: func(*App) { next(value) }}
	a.dirty = true
}

// Machine forms retain main's cursor editing independently of rename inputs.
func (a *App) machineInputKey(k uv.Key) {
	d := a.dialog
	runes := []rune(*d.input)
	cursor := min(max(0, d.d.InputCursor), len(runes))
	switch {
	case k.Code == 'c' && k.Mod.Contains(uv.ModCtrl):
		runes, cursor = nil, 0
	case k.Code == uv.KeyLeft:
		cursor = max(0, cursor-1)
	case k.Code == uv.KeyRight:
		cursor = min(len(runes), cursor+1)
	case k.Code == uv.KeyHome:
		cursor = 0
	case k.Code == uv.KeyEnd:
		cursor = len(runes)
	case k.Code == uv.KeyBackspace:
		if cursor > 0 {
			runes = append(runes[:cursor-1], runes[cursor:]...)
			cursor--
		}
	case k.Code == uv.KeyDelete:
		if cursor < len(runes) {
			runes = append(runes[:cursor], runes[cursor+1:]...)
		}
	case k.Text != "":
		insert := []rune(k.Text)
		runes = append(append(append([]rune{}, runes[:cursor]...), insert...), runes[cursor:]...)
		cursor += len(insert)
	}
	*d.input = string(runes)
	d.d.Input, d.d.InputCursor = *d.input, cursor
}

func (a *App) addMachineDialog() {
	a.machineInput("SSH target", "", func(target string) {
		a.machineInput("Display name", "", func(label string) {
			a.machineInput("Remote session", "default", func(session string) { a.probeMachine(target, label, session) })
		})
	})
}

func (a *App) probeMachine(target, label, session string) {
	c := a.machines()
	if err := machine.ValidateAdd(c.Load(), target, label, session); err != nil {
		a.machineError(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	pending := &uiDialog{d: render.Dialog{Title: "Checking SSH reachability", Detail: target, Confirm: "cancel", Cancel: "cancel"}, cancel: func(*App) { cancel() }, run: func(*App) { cancel() }}
	a.dialog = pending
	a.dirty = true
	go func() {
		err := machine.Probe(ctx, target)
		if ctx.Err() != nil {
			cancel()
			return
		}
		a.Do(func() {
			defer cancel()
			if ctx.Err() != nil || a.dialog != pending {
				return
			}
			if err != nil {
				a.machineError(err)
				return
			}
			p, err := c.AddVerified(target, label, session)
			if err != nil {
				a.machineError(err)
				return
			}
			a.dialog = &uiDialog{d: render.Dialog{Title: fmt.Sprintf("Saved SSH machine %s.", p.ID), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
			a.dirty = true
		})
	}()
}

func (a *App) removeMachineMenu() {
	profiles := a.machines().Load()
	if len(profiles) == 0 {
		a.machineError(fmt.Errorf("No saved SSH machines."))
		return
	}
	items := make([]render.MenuItem, len(profiles))
	for i, p := range profiles {
		items[i] = render.MenuItem{Label: p.Label + " (" + p.Target + ")"}
	}
	a.menu = &uiMenu{kind: menuMachineRemove, machines: profiles, m: render.Menu{X: 2, Y: 2, Hover: -1, Sel: -1, Items: items}}
	a.dirty = true
}

func (a *App) removeMachineDialog(p machine.Profile) {
	a.dialog = &uiDialog{d: render.Dialog{Title: "remove machine?", Detail: p.Label + " (" + p.Target + ")", Confirm: "remove", Cancel: "cancel"}, run: func(a *App) {
		if _, err := a.machines().Change(p.ID, "remove", "", &p); err != nil {
			a.machineError(err)
			return
		}
		a.dirty = true
	}}
	a.dirty = true
}
