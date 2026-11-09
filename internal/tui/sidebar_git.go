package tui

import (
	"context"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/filewatch"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/worktree"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type workspaceGitTarget struct {
	id             int
	cwd, nameCwd   string
	generation     string
	paneID         int
	paneGeneration string
	pane           *runtime.Pane
}

func (a *App) captureWorkspaceGitTarget(w *model.Workspace) workspaceGitTarget {
	t := workspaceGitTarget{id: w.ID, cwd: w.Cwd, nameCwd: a.workspaceNameCwd(w), generation: w.Generation}
	if len(w.Tabs) > 0 {
		ids := w.Tabs[0].Layout.PaneIDs()
		if len(ids) > 0 {
			t.paneID = ids[0]
			t.paneGeneration = a.st.PaneGeneration(t.paneID)
			t.pane = a.sup.Get(t.paneID)
		}
	}
	return t
}

func (a *App) currentWorkspaceGitTarget(w *model.Workspace, t workspaceGitTarget) bool {
	if w.ID != t.id || w.Generation != t.generation || w.Cwd != t.cwd {
		return false
	}
	var paneID int
	if len(w.Tabs) > 0 {
		ids := w.Tabs[0].Layout.PaneIDs()
		if len(ids) > 0 {
			paneID = ids[0]
		}
	}
	if paneID != t.paneID || a.st.PaneGeneration(paneID) != t.paneGeneration || a.sup.Get(paneID) != t.pane {
		return false
	}
	if t.pane != nil {
		return a.workspaceNameCwd(w) == t.nameCwd
	}
	return true
}

// refreshStaleWorktrees re-anchors a space whose first pane's shell has left
// the checkout its worktree metadata names (for example a reused space). The
// space's directory moves with it, so the git line and worktree actions use
// the same checkout as the grouping.
func (a *App) refreshStaleWorktrees(targets []workspaceGitTarget, entries map[string]gitCwdActivity) {
	index := make(map[int]int, len(a.st.Workspaces))
	for i, w := range a.st.Workspaces {
		index[w.ID] = i
	}
	for _, t := range targets {
		i, ok := index[t.id]
		if ok && a.currentWorkspaceGitTarget(a.st.Workspaces[i], t) {
			a.refreshStaleWorktreeAt(i, t, entries)
		}
	}
}

func (a *App) refreshStaleWorktreeAt(i int, t workspaceGitTarget, entries map[string]gitCwdActivity) {
	if t.nameCwd == t.cwd {
		return // no live shell directory beyond the space's directory
	}
	if a.worktreePending[workspaceIdentity{id: t.id, generation: t.generation}] > 0 {
		return
	}
	info := a.st.Workspaces[i].Worktree
	entry, ok := entries[t.nameCwd]
	if info == nil || !ok || !entry.loaded || worktree.Within(t.nameCwd, info.CheckoutPath) {
		return
	}
	var next *api.WorkspaceWorktreeInfo
	if entry.sourceErr == nil {
		next = worktree.Metadata(entry.source, entry.source.SourceCheckoutPath)
	}
	a.exec(model.CmdSetWorktree{Index: i, Info: next, Cwd: t.nameCwd})
	a.dirty = true
}

type workspacePreview struct {
	revision uint64
	line     string
}

// workspacePreviewLines reads one line per space from its first tab's first
// pane. Each screen chooses its own density, so the line is kept for all of
// them; a pane is read again only after it printed something.
func (a *App) workspacePreviewLines() map[int]string {
	if a.sidebarConfig.UI.Sidebar.Spaces.Rows != nil {
		return nil
	}
	out := map[int]string{}
	seen := map[int]bool{}
	for _, w := range a.st.Workspaces {
		if len(w.Tabs) == 0 {
			continue
		}
		ids := w.Tabs[0].Layout.PaneIDs()
		if len(ids) == 0 {
			continue
		}
		p := a.sup.Get(ids[0])
		if p == nil {
			continue
		}
		seen[ids[0]] = true
		revision := p.OutputRevision()
		cached, ok := a.previewCache[ids[0]]
		if !ok || cached.revision != revision {
			cached = workspacePreview{revision, p.PreviewLine()}
			if a.previewCache == nil {
				a.previewCache = map[int]workspacePreview{}
			}
			a.previewCache[ids[0]] = cached
		}
		if cached.line != "" {
			out[w.ID] = cached.line
		}
	}
	for pid := range a.previewCache {
		if !seen[pid] {
			delete(a.previewCache, pid)
		}
	}
	return out
}

type workspaceDirectoryLabel struct {
	cwd, name string
}

func (a *App) reindexWorkspaceMetadata(previousIDs []int) {
	names := map[int]string{}
	git := map[int]render.GitStatus{}
	for i, id := range previousIDs {
		names[id] = a.workspaceNames[i]
		git[id] = a.workspaceGit[i]
	}
	a.workspaceNames = map[int]string{}
	a.workspaceGit = map[int]render.GitStatus{}
	labels := make(map[int]workspaceDirectoryLabel, len(a.st.Workspaces))
	for i, ws := range a.st.Workspaces {
		a.workspaceNames[i] = names[ws.ID]
		a.workspaceGit[i] = git[ws.ID]
		if label, ok := a.workspaceLabels[ws.ID]; ok {
			labels[ws.ID] = label
		}
	}
	a.workspaceLabels = labels
}

// Git metadata follows the observed 1.5-second sidebar refresh cadence;
// never launch git from frame composition or block pane input on it.
func (a *App) refreshWorkspaceGit(now time.Time) {
	if now.Sub(a.gitAt) < 1500*time.Millisecond {
		return
	}
	a.gitAt = now
	if a.gitActivity == nil {
		a.gitActivity = &gitActivityState{entries: map[string]gitCwdActivity{}, running: map[string]context.CancelFunc{}}
	}
	var targets []workspaceGitTarget
	wanted := map[string]bool{}
	for _, w := range a.st.Workspaces {
		target := a.captureWorkspaceGitTarget(w)
		nameCwd := target.nameCwd
		targets = append(targets, target)
		wanted[w.Cwd], wanted[nameCwd] = true, true
	}
	state := a.gitActivity
	pending := map[string]gitCwdActivity{}
	removed := map[string]gitCwdActivity{}
	for cwd := range wanted {
		entry, ok := state.entries[cwd]
		if !ok {
			entry.watch = filewatch.New(nil)
			state.entries[cwd] = entry
		}
		if state.running[cwd] == nil {
			pending[cwd] = entry
		}
	}
	for cwd, entry := range state.entries {
		if !wanted[cwd] {
			if cancel := state.running[cwd]; cancel != nil {
				cancel()
			} else {
				removed[cwd] = entry
			}
			delete(state.entries, cwd)
		}
	}
	positions := make(map[int]int, len(a.st.Workspaces))
	byCwd := make(map[string][]workspaceGitTarget)
	for i, target := range targets {
		positions[target.id] = i
		byCwd[target.cwd] = append(byCwd[target.cwd], target)
		if target.nameCwd != target.cwd {
			byCwd[target.nameCwd] = append(byCwd[target.nameCwd], target)
		}
	}
	if a.workspaceGit == nil {
		a.workspaceGit = map[int]render.GitStatus{}
	}
	if a.workspaceNames == nil {
		a.workspaceNames = map[int]string{}
	}
	if a.workspaceLabels == nil {
		a.workspaceLabels = map[int]workspaceDirectoryLabel{}
	}
	apply := func(updated map[string]gitCwdActivity) {
		for cwd, entry := range updated {
			state.entries[cwd] = entry
		}
		var selected []workspaceGitTarget
		if updated == nil {
			selected = targets
		} else {
			for cwd := range updated {
				selected = append(selected, byCwd[cwd]...)
			}
		}
		for _, t := range selected {
			i, ok := positions[t.id]
			if !ok || i >= len(a.st.Workspaces) {
				continue
			}
			w := a.st.Workspaces[i]
			if !a.currentWorkspaceGitTarget(w, t) {
				continue
			}
			git := state.entries[t.cwd].git
			label := workspaceDirectoryLabel{t.nameCwd, state.entries[t.nameCwd].name}
			name := label.name
			if name == "" {
				name = filepath.Base(w.Cwd)
			}
			if w.CustomName {
				name = w.Name
			}
			if !w.CustomName && len(w.Tabs) > 0 {
				ids := w.Tabs[0].Layout.PaneIDs()
				if len(ids) > 0 {
					if p := a.sup.Get(ids[0]); p != nil {
						sn := p.Snapshot()
						agent := sn.AgentID
						if r := a.reported[ids[0]]; r.agent != "" {
							agent = r.agent
						}
						title := sn.OSCTitle
						if rec := a.sessions[ids[0]]; rec != nil && rec.Title != "" {
							title = rec.Title
						}
						if a.paneMetadataSource != nil {
							if m := a.paneMetadataSource(ids[0]); m.Title != "" {
								title = m.Title
							}
						}
						if agent != "" && title != "" && title != name {
							name += "-" + title
						}
					}
				}
			}
			if a.workspaceGit[i] != git || a.workspaceLabels[w.ID] != label || a.workspaceNames[i] != name {
				a.dirty = true
			}
			a.workspaceGit[i], a.workspaceLabels[w.ID], a.workspaceNames[i] = git, label, name
			a.refreshStaleWorktreeAt(i, t, state.entries)
		}
		a.gitPending = len(state.running) > 0
	}
	previews := a.workspacePreviewLines()
	if !reflect.DeepEqual(previews, a.workspacePreviews) {
		a.workspacePreviews = previews
		a.dirty = true
	}

	if len(pending) == 0 && len(removed) == 0 {
		apply(nil)
		return
	}
	a.gitPending = len(state.running) > 0 || len(pending) > 0
	for _, entry := range removed {
		entry.watch.Close()
	}
	for cwd, entry := range pending {
		ctx, cancel := context.WithCancel(context.Background())
		state.running[cwd] = cancel
		go func(ctx context.Context, cancel context.CancelFunc, cwd string, entry gitCwdActivity) {
			defer cancel()
			if entry.loaded && entry.ready {
				var ok bool
				entry, ok = unchangedGitActivity(entry)
				if !ok {
					entry = readGitActivity(ctx, cwd, entry)
				}
			} else {
				entry = readGitActivity(ctx, cwd, entry)
			}
			if ctx.Err() != nil {
				entry.watch.Close()
			}
			a.Do(func() {
				delete(state.running, cwd)
				if a.gitActivity != state {
					entry.watch.Close()
					return
				}
				if current, ok := state.entries[cwd]; !ok || current.watch != entry.watch || ctx.Err() != nil {
					entry.watch.Close()
					apply(nil)
					return
				}
				apply(map[string]gitCwdActivity{cwd: entry})
			})
		}(ctx, cancel, cwd, entry)
	}
}

func (a *App) workspaceNameCwd(w *model.Workspace) string {
	if len(w.Tabs) > 0 {
		ids := w.Tabs[0].Layout.PaneIDs()
		if len(ids) > 0 {
			if p := a.sup.Get(ids[0]); p != nil {
				if cwd := platform.ProcCwd(p.ShellPID()); cwd != "" {
					return cwd
				}
			}
		}
	}
	return w.Cwd
}

func workspaceDirectoryName(cwd string) string {
	if src, err := worktree.Source(cwd); err == nil {
		return filepath.Base(src.SourceCheckoutPath)
	}
	return filepath.Base(cwd)
}

// WorkspaceLabel never runs Git on the input loop. Checkout names arrive from
// the metadata worker; a changed directory immediately gets its basename.
func (a *App) WorkspaceLabel(index int) string {
	w := a.st.Workspaces[index]
	if w.CustomName {
		return w.Name
	}
	cwd := a.workspaceNameCwd(w)
	if label, ok := a.workspaceLabels[w.ID]; ok && label.cwd == cwd {
		return label.name
	}
	return filepath.Base(cwd)
}
func readWorkspaceGit(ctx context.Context, cwd string) render.GitStatus {
	var g render.GitStatus
	run := func(args ...string) string {
		cmd := childenv.CommandContextGroup(ctx, "git", append([]string{"-C", cwd}, args...)...)
		b, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	g.Branch = run("symbolic-ref", "--quiet", "--short", "HEAD")
	counts := strings.Fields(run("rev-list", "--left-right", "--count", "HEAD...@{upstream}"))
	if len(counts) == 2 {
		g.Ahead, _ = strconv.Atoi(counts[0])
		g.Behind, _ = strconv.Atoi(counts[1])
	}
	g.Added, g.Deleted = readWorkspaceGitDiff(ctx, cwd)
	return g
}

func readWorkspaceGitDiff(ctx context.Context, cwd string) (added, deleted int) {
	output, err := gitActivityOutput(ctx, cwd, "diff", "--numstat", "HEAD", "--")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		add, e1 := strconv.Atoi(fields[0])
		del, e2 := strconv.Atoi(fields[1])
		if e1 == nil && e2 == nil {
			added += add
			deleted += del
		}
	}
	return added, deleted
}
