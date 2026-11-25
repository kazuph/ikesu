package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kazuph/ikesu/internal/model"
)

// CheckPruneCwd rejects new uses of the reserved checkout. Call on the app
// loop, in the same turn that would admit the workspace or process.
func (a *App) CheckPruneCwd(cwd string) error {
	owner := a
	if a.viewerOwner != nil {
		owner = a.viewerOwner
	}
	if owner.pruneRemoving == nil {
		return nil
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
		if cwd == "" {
			cwd = os.Getenv("HOME")
		}
	}
	if withinPruneDirectory(cwd, owner.pruneRemoving) {
		return fmt.Errorf("checkout is being pruned: %s", cwd)
	}
	return nil
}

// Resolve symlinks from the nearest existing ancestor. Walk its parent
// directories by filesystem identity so different spellings of an existing
// directory compare correctly on either kind of volume.
func withinPruneDirectory(path string, root os.FileInfo) bool {
	if path == "" {
		return false
	}
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path, err = filepath.Abs(resolved)
			if err != nil {
				return false
			}
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
	for {
		if info, err := os.Stat(path); err == nil && os.SameFile(info, root) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func (a *App) pruneCommandAdmission(cmd model.Command, policyCwd bool, spawnCwd string) error {
	owner := a
	if a.viewerOwner != nil {
		owner = a.viewerOwner
	}
	if owner.pruneRemoving == nil {
		return nil
	}
	workspaceCwd := func(index int) string {
		if index >= 0 && index < len(a.st.Workspaces) {
			return a.st.Workspaces[index].Cwd
		}
		return ""
	}
	paneCwd := func(cwd string, index int) string {
		if cwd == "" {
			cwd = workspaceCwd(index)
		}
		if policyCwd {
			if spawnCwd != "" {
				cwd = spawnCwd
			}
			cwd = a.newPaneCwd(cwd)
		}
		return cwd
	}

	switch c := cmd.(type) {
	case model.CmdNewWorkspace:
		return a.CheckPruneCwd(c.Cwd)
	case model.CmdNewTab:
		return a.CheckPruneCwd(paneCwd(c.Cwd, c.Workspace))
	case model.CmdSplitFocused:
		return a.CheckPruneCwd(paneCwd("", a.st.Active))
	case model.CmdSplitPane:
		wi, _ := a.st.FindPane(c.Pane)
		return a.CheckPruneCwd(paneCwd(c.Cwd, wi))
	case model.CmdSplitImplicit:
		return a.CheckPruneCwd(paneCwd(c.Cwd, c.Workspace))
	case model.CmdOpenOverlay:
		return a.CheckPruneCwd(c.Cwd)
	case model.CmdOpenPopup:
		return a.CheckPruneCwd(c.Cwd)
	case model.CmdDuplicateWorkspace:
		if err := a.CheckPruneCwd(workspaceCwd(c.Index)); err != nil {
			return err
		}
		for _, cwd := range c.Cwds {
			if err := a.CheckPruneCwd(paneCwd(cwd, c.Index)); err != nil {
				return err
			}
		}
	case model.CmdApplyLayout:
		var check func(*model.ApplyNode) error
		check = func(node *model.ApplyNode) error {
			if node == nil {
				return nil
			}
			if node.Leaf != nil {
				return a.CheckPruneCwd(paneCwd(node.Leaf.Cwd, c.Workspace))
			}
			if err := check(node.First); err != nil {
				return err
			}
			return check(node.Second)
		}
		return check(c.Root)
	}
	return nil
}
