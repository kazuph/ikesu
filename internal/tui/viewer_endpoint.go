package tui

import (
	"errors"

	"github.com/kazuph/ikesu/internal/model"
)

// SetLegacySidebarControls selects the geometry understood by this viewer's client.
func (v *Viewer) SetLegacySidebarControls(legacy, menuRight bool) {
	v.app.legacySidebarControls, v.app.sidebarMenuRight = legacy, menuRight
}

// Hidden machine endpoints still supply sidebar snapshots, but cannot mark
// panes seen or receive clipboard writes carried in terminal output.
func (v *Viewer) SetVisible(a *App, visible bool) {
	v.hidden = !visible
	if !visible {
		v.app.windowBlurred = true
		return
	}
	if v.app.windowBlurred {
		// Queries emitted while this machine was hidden did not reach the host.
		v.with(a, func(ui *App) { ui.queryHostMouse() })
	}
	v.Input(a, []byte("\x1b[I"))
	if v.snapshotCopyCanceled {
		v.snapshotCopyCanceled = false
		v.with(a, func(ui *App) {
			ui.ShowToast("Copy handoff snapshot", "画面が切り替わったためコピーを取り消しました。もう一度選んでください", "error", -1)
		})
	}
}

// SelectWorkspace uses the remote workspace's original ID and changes only
// this viewer's navigation, preserving the session's canonical selection.
func (v *Viewer) SelectWorkspace(a *App, id int) bool {
	for index, workspace := range a.st.Workspaces {
		if workspace.ID == id {
			v.with(a, func(ui *App) { ui.exec(model.CmdSelectWorkspace{Index: index}) })
			return true
		}
	}
	return false
}

// NewWorkspace creates a workspace using this viewer's current selection and cwd.
func (v *Viewer) NewWorkspace(a *App) {
	v.with(a, func(ui *App) { ui.newWorkspace() })
}

// NewSectionWorkspace creates a workspace in the requested sidebar section.
func (v *Viewer) NewSectionWorkspace(a *App, section model.Section) {
	v.with(a, func(ui *App) { ui.newSectionWorkspace(section) })
}

// ToggleSection folds or unfolds a sidebar section regardless of scroll offset.
func (v *Viewer) ToggleSection(a *App, section model.Section) {
	v.with(a, func(ui *App) { ui.toggleSection(section) })
}

func (v *Viewer) HasWorkspace(a *App, id int, generation string) bool {
	for _, ws := range a.st.Workspaces {
		if ws.ID == id && generation != "" && ws.Generation == generation {
			return true
		}
	}
	return false
}

func (a *App) isOyagoiWorkspace(ws *model.Workspace) bool {
	return a.oyagoiWorkspace != 0 && ws.ID == a.oyagoiWorkspace && a.oyagoiGeneration != "" && ws.Generation == a.oyagoiGeneration
}

func (v *Viewer) OpenWorkspaceMenu(a *App, id int, generation string, x, y int) bool {
	for index, ws := range a.st.Workspaces {
		if ws.ID == id && generation != "" && ws.Generation == generation {
			v.with(a, func(ui *App) { ui.openWorkspaceMenu(x, y, index); ui.dirty = true })
			return true
		}
	}
	return false
}

func (v *Viewer) SetOyagoi(a *App, id int, generation string) {
	chosen := 0
	for _, ws := range a.st.Workspaces {
		if id != 0 && ws.ID == id && generation != "" && ws.Generation == generation {
			chosen = id
			break
		}
	}
	v.with(a, func(ui *App) { ui.oyagoiWorkspace = chosen; ui.oyagoiGeneration = generation; ui.dirty = true })
}

func (v *Viewer) SetForeignOyagoi(a *App, label string, online bool) {
	v.with(a, func(ui *App) { ui.oyagoiForeignName, ui.oyagoiForeignOnline = label, online; ui.dirty = true })
}

func (v *Viewer) SetOyagoiCollapsed(a *App, collapsed bool) {
	v.with(a, func(ui *App) { ui.oyagoiCollapsed = collapsed; ui.dirty = true })
}

// FocusPane navigates only this viewer, and checks the resulting input target.
func (v *Viewer) FocusPane(a *App, pane int) error {
	blocked := errors.New("対象のタブで別のポップアップが開いているため移動できません。閉じてから移動してください")
	var err error
	v.with(a, func(ui *App) {
		s := ui.st
		wi, ti := s.FindManagedPane(pane)
		if wi < 0 {
			err = errors.New("移動先のペインを確認できません")
			return
		}
		ws := s.Workspaces[wi]
		if ti < 0 {
			ti = ws.ActiveTab
		}
		// Check the destination before changing selection so a failed
		// request also preserves the viewer's current input target.
		if o := s.Overlay; o != nil && o.WorkspaceID == ws.ID && o.TabID == ws.Tabs[ti].ID && o.PaneID != pane {
			err = blocked
			return
		}
		popup := s.Popups[ws.ID]
		if popup == nil && s.Popup != nil && s.Popup.WorkspaceID == ws.ID {
			popup = s.Popup
		}
		if popup != nil && popup.PaneID != pane && (s.Overlay == nil || s.Overlay.PaneID != pane) {
			err = blocked
			return
		}
		ui.focusPane(pane)
		if s.FocusedManagedPane() != pane {
			err = blocked
		}
	})
	return err
}
