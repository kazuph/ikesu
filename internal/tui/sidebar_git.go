package tui

import (
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/worktree"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
	for i, ws := range a.st.Workspaces {
		a.workspaceNames[i] = names[ws.ID]
		a.workspaceGit[i] = git[ws.ID]
	}
}

// Git metadata follows the observed 1.5-second sidebar refresh cadence;
// never launch git from frame composition or block pane input on it.
func (a *App) refreshWorkspaceGit(now time.Time) {
	if a.gitPending || now.Sub(a.gitAt) < 1500*time.Millisecond {
		return
	}
	a.gitPending = true
	a.gitAt = now
	type target struct {
		id      int
		cwd     string
		nameCwd string
	}
	var targets []target
	for _, w := range a.st.Workspaces {
		targets = append(targets, target{w.ID, w.Cwd, a.workspaceNameCwd(w)})
	}
	go func() {
		gs := map[int]render.GitStatus{}
		names := map[int]workspaceDirectoryLabel{}
		for _, w := range targets {
			gs[w.id] = readWorkspaceGit(w.cwd)
			names[w.id] = workspaceDirectoryLabel{w.nameCwd, workspaceDirectoryName(w.nameCwd)}
		}
		a.Do(func() {
			a.workspaceLabels = names
			a.workspaceGit = map[int]render.GitStatus{}
			a.workspaceNames = map[int]string{}
			for i, w := range a.st.Workspaces {
				a.workspaceGit[i] = gs[w.ID]
				name := names[w.ID].name
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
				a.workspaceNames[i] = name
			}
			a.gitPending = false
			a.dirty = true
		})
	}()
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
func readWorkspaceGit(cwd string) render.GitStatus {
	var g render.GitStatus
	run := func(args ...string) string {
		cmd := childenv.Command("git", append([]string{"-C", cwd}, args...)...)
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
	for _, line := range strings.Split(run("diff", "--numstat", "HEAD", "--"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		add, e1 := strconv.Atoi(fields[0])
		del, e2 := strconv.Atoi(fields[1])
		if e1 == nil && e2 == nil {
			g.Added += add
			g.Deleted += del
		}
	}
	return g
}
