package tui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/worktree"
)

const pruneCheckoutTimeout = 30 * time.Second

// pruneProtection returns the private selection and HQ of an attached viewer.
// The caller reads it on the app loop, like other viewer state.
func (v *Viewer) pruneProtection() (focused, hq int, generation string) {
	return v.workspace, v.app.oyagoiWorkspace, v.app.oyagoiGeneration
}

type pruneTarget struct {
	candidate endpoint.PruneCandidate
	root      string
	repo      string
	directory os.FileInfo
}

func (a *App) pruneTarget(ws *model.Workspace, viewers []*Viewer, uses pruneUses) (pruneTarget, string) {
	info := ws.Worktree
	if info == nil || !info.IsLinkedWorktree {
		return pruneTarget{}, "not a linked worktree"
	}
	if len(a.st.Workspaces) <= 1 {
		return pruneTarget{}, "last workspace"
	}
	if a.isOyagoiWorkspace(ws) {
		return pruneTarget{}, "HQ"
	}
	focused := a.st.ActiveWorkspace() == ws
	for _, viewer := range viewers {
		selected, hq, generation := viewer.pruneProtection()
		if hq == ws.ID && generation == ws.Generation {
			return pruneTarget{}, "HQ"
		}
		focused = focused || selected == ws.ID
	}
	if focused {
		return pruneTarget{}, "focused"
	}
	if ws.Generation == "" || !filepath.IsAbs(info.CheckoutPath) || !filepath.IsAbs(info.RepoRoot) ||
		!worktree.Within(ws.Cwd, info.CheckoutPath) || a.worktreePending[workspaceIdentity{id: ws.ID, generation: ws.Generation}] != 0 {
		return pruneTarget{}, "workspace changed"
	}
	dir, err := os.Stat(info.CheckoutPath)
	if err != nil || !dir.IsDir() {
		return pruneTarget{}, "checkout unavailable"
	}
	if reason := uses.protection(dir, ws.ID); reason != "" {
		return pruneTarget{}, reason
	}
	name := render.WorkspaceDisplayName(ws, nil)
	hash := sha256.New()
	addPane := func(id int) bool {
		state := a.PaneAgentStatusKey(id)
		if a.decisionAttention[id] || state == "working" || state == "blocked" {
			return false
		}
		var activity uint64
		var agent string
		var foreground int
		if a.sup != nil {
			if pane := a.sup.Get(id); pane != nil {
				_, activity = pane.IdleActivity()
				snap := pane.Snapshot()
				agent, foreground = agentFor(snap), snap.FgPID
				if (agent != "" || a.reported[id].agent != "") && state == "unknown" {
					return false
				}
			}
		}
		fmt.Fprintf(hash, "%d %q %q %d %d %q %q %d\n", id, a.st.PaneGeneration(id), state, a.AgentStateChangeSeq(id), activity, agent, a.reported[id].agent, foreground)
		return true
	}
	for _, id := range a.st.WorkspacePaneIDs(ws) {
		if !addPane(id) {
			return pruneTarget{}, "active agent"
		}
	}
	return pruneTarget{
		candidate: endpoint.PruneCandidate{
			Workspace: ws.ID, Generation: ws.Generation, PaneState: fmt.Sprintf("%x", hash.Sum(nil)),
			Name: name, Path: info.CheckoutPath,
		},
		root: info.RepoRoot, repo: info.RepoKey, directory: dir,
	}, ""
}

type pruneUses struct {
	workspaceCwds map[int][]string
	jobCwds       []string
	jobErr        error
}

// SetPruneJobCwds installs the authoritative background job lookup in server
// mode. The lookup runs on the app loop, which also admits new jobs.
func (a *App) SetPruneJobCwds(source func() ([]string, error)) {
	a.pruneJobCwds = source
}

func (a *App) pruneCwds() pruneUses {
	uses := pruneUses{workspaceCwds: make(map[int][]string, len(a.st.Workspaces))}
	if a.pruneJobCwds != nil {
		uses.jobCwds, uses.jobErr = a.pruneJobCwds()
	}
	for _, ws := range a.st.Workspaces {
		paths := []string{ws.Cwd}
		if ws.Worktree != nil {
			paths = append(paths, ws.Worktree.CheckoutPath)
		}
		if a.sup != nil {
			for _, id := range a.st.WorkspacePaneIDs(ws) {
				if pane := a.sup.Get(id); pane != nil {
					paths = append(paths, pane.LiveCwds()...)
				}
			}
		}
		uses.workspaceCwds[ws.ID] = paths
	}
	return uses
}

func (uses pruneUses) protection(dir os.FileInfo, except int) string {
	if uses.jobErr != nil {
		return "background job check failed"
	}
	for _, cwd := range uses.jobCwds {
		if withinPruneDirectory(cwd, dir) {
			return "checkout used by a background job"
		}
	}
	for id, paths := range uses.workspaceCwds {
		if id == except {
			continue
		}
		for _, cwd := range paths {
			if withinPruneDirectory(cwd, dir) {
				return "checkout used by another workspace"
			}
		}
	}
	return ""
}

// PruneScan captures session state on the app loop and does Git and disk I/O
// off it. viewers is evaluated on the app loop to include current selections.
func (a *App) PruneScan(ctx context.Context, generation string, viewers func() []*Viewer) endpoint.PruneScanResult {
	result := endpoint.PruneScanResult{ServerGeneration: generation, Protected: map[string]int{}}
	var targets []pruneTarget
	if !a.DoContext(ctx, func() {
		attached, cwds := viewers(), a.pruneCwds()
		for _, ws := range a.st.Workspaces {
			if ws.Worktree == nil || !ws.Worktree.IsLinkedWorktree {
				continue
			}
			target, reason := a.pruneTarget(ws, attached, cwds)
			if reason != "" {
				result.Protected[reason]++
			} else {
				targets = append(targets, target)
			}
		}
	}) {
		return result
	}
	repos := map[string]*worktree.PruneRepository{}
	for _, target := range targets {
		if ctx.Err() != nil {
			return result
		}
		candidate, reason := scanPruneTarget(ctx, repos, target)
		if reason == "" {
			result.Candidates = append(result.Candidates, candidate)
		} else {
			result.Protected[reason]++
		}
	}
	return result
}

func scanPruneTarget(ctx context.Context, repos map[string]*worktree.PruneRepository, target pruneTarget) (endpoint.PruneCandidate, string) {
	ctx, cancel := context.WithTimeout(ctx, pruneCheckoutTimeout)
	defer cancel()

	candidate := target.candidate
	repo, ok := repos[target.root]
	if !ok {
		repo, _ = worktree.ReadPruneRepository(ctx, target.root)
		repos[target.root] = repo
	}
	if repo == nil {
		return candidate, "Git check failed"
	}
	revision, reason, err := repo.Check(ctx, candidate.Path)
	if err != nil {
		return candidate, "Git check failed"
	}
	if reason != "" {
		return candidate, reason
	}
	candidate.Revision = revision
	candidate.Bytes, err = worktree.DiskUsage(ctx, candidate.Path)
	if err != nil {
		return candidate, "size check failed"
	}
	return candidate, ""
}

func (a *App) PruneExec(ctx context.Context, generation string, request endpoint.PruneExecRequest, viewers func() []*Viewer, allowed func() bool) endpoint.PruneExecResult {
	result := endpoint.PruneExecResult{}
	for _, candidate := range request.Candidates {
		outcome := endpoint.PruneOutcome{Workspace: candidate.Workspace, State: endpoint.PruneSkipped, Reason: "server changed"}
		if generation != "" && request.ServerGeneration == generation {
			outcome = a.pruneOne(ctx, candidate, viewers, allowed)
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result
}

// refresh validates the same workspace and pane state on both sides of the
// filesystem checks. Closing executes on the app loop with the final check.
func (a *App) pruneOne(ctx context.Context, candidate endpoint.PruneCandidate, viewers func() []*Viewer, allowed func() bool) endpoint.PruneOutcome {
	outcome := endpoint.PruneOutcome{Workspace: candidate.Workspace, State: endpoint.PruneSkipped}
	checkoutCtx, cancel := context.WithTimeout(ctx, pruneCheckoutTimeout)
	defer cancel()
	var root, repo string
	var directory os.FileInfo
	refresh := func(closeSpace bool) string {
		reason := "server unavailable"
		a.DoContext(checkoutCtx, func() {
			if !allowed() || a.handoffStarting {
				return
			}
			reason = "workspace changed"
			for i, ws := range a.st.Workspaces {
				if ws.ID != candidate.Workspace || candidate.Generation == "" || ws.Generation != candidate.Generation {
					continue
				}
				target, protection := a.pruneTarget(ws, viewers(), a.pruneCwds())
				if protection != "" {
					reason = protection
				} else if target.candidate.Path == candidate.Path && target.candidate.PaneState == candidate.PaneState {
					if !closeSpace {
						root, repo, directory = target.root, target.repo, target.directory
						reason = ""
					} else if a.worktreeMutations != 0 {
						reason = "another worktree operation is in progress"
					} else if root == target.root && repo == target.repo && os.SameFile(directory, target.directory) {
						a.pruneRemoving = target.directory
						a.worktreeMutations++
						a.exec(model.CmdCloseWorkspace{Index: i})
						reason = ""
					}
				}
				break
			}
		})
		return reason
	}
	if outcome.Reason = refresh(false); outcome.Reason != "" {
		return outcome
	}
	// Capacity measurement can take significantly longer than Git checks.
	// Finish it before re-reading Git immediately before closing the space.
	bytes, err := worktree.DiskUsage(checkoutCtx, candidate.Path)
	if err != nil {
		outcome.Reason = "size check failed"
		return outcome
	}
	if err := checkPruneRevision(checkoutCtx, root, candidate); err != nil {
		outcome.Reason = err.Error()
		return outcome
	}
	if outcome.Reason = refresh(true); outcome.Reason != "" {
		return outcome
	}

	defer a.Do(func() {
		a.pruneRemoving = nil
		a.worktreeMutations--
	})

	// A fresh Git check after closing catches a checkout or branch change while
	// the panes shut down. Git still has the final dirty/locked-worktree check.
	removeCtx, cancelRemove := context.WithTimeout(context.WithoutCancel(ctx), pruneCheckoutTimeout)
	defer cancelRemove()
	err = checkPruneRevision(removeCtx, root, candidate)
	if err == nil {
		reason := "server unavailable"
		a.DoContext(removeCtx, func() { reason = a.pruneCwds().protection(directory, 0) })
		if reason != "" {
			err = errors.New(reason)
		} else if current, statErr := os.Stat(candidate.Path); statErr != nil || !os.SameFile(directory, current) {
			err = errors.New("checkout directory changed")
		} else {
			err = worktree.RemoveContext(removeCtx, root, candidate.Path, false)
		}
	}
	if err != nil {
		outcome.State, outcome.Reason = endpoint.PruneFailed, "workspace closed; checkout removal failed: "+err.Error()
		return outcome
	}
	outcome.State, outcome.Bytes = endpoint.PruneDeleted, bytes
	return outcome
}

func checkPruneRevision(ctx context.Context, root string, candidate endpoint.PruneCandidate) error {
	repo, err := worktree.ReadPruneRepository(ctx, root)
	if err != nil {
		return err
	}
	revision, reason, err := repo.Check(ctx, candidate.Path)
	if err != nil {
		return err
	}
	if reason != "" {
		return errors.New(reason)
	}
	if revision != candidate.Revision {
		return errors.New("branch or base changed")
	}
	return nil
}

const standalonePruneGeneration = "standalone"

func (a *App) openLocalPrune() {
	a.prune = NewPruneDialog("Local", []string{"Local"})
	a.dirty = true
}

func (a *App) localPruneInput(ev input.Event) {
	d := a.prune
	switch d.Input(ev, layout.Rect{W: a.st.Cols, H: a.st.Rows}) {
	case PruneClose:
		if a.pruneCancel != nil {
			a.pruneCancel()
			a.pruneCancel = nil
		}
		a.prune = nil
	case PruneStartScan:
		ctx, cancel := context.WithCancel(context.Background())
		a.pruneCancel = cancel
		go func() {
			defer cancel()
			result := a.PruneScan(ctx, standalonePruneGeneration, noPruneViewers)
			a.DoContext(ctx, func() {
				if a.prune == d {
					a.pruneCancel = nil
					d.AddScan("local", "Local", result)
					d.Ready()
					a.dirty = true
				}
			})
		}()
	case PruneStartExec:
		request := endpoint.PruneExecRequest{ServerGeneration: standalonePruneGeneration}
		names := map[int]string{}
		for _, entry := range d.Selection() {
			candidate := entry.Candidate
			request.Candidates = append(request.Candidates, candidate)
			names[candidate.Workspace] = candidate.Name
		}
		go func() {
			result := a.PruneExec(context.Background(), standalonePruneGeneration, request, noPruneViewers, func() bool { return a.prune == d })
			a.Do(func() {
				for _, outcome := range result.Outcomes {
					d.AddOutcome("Local", names[outcome.Workspace], outcome)
				}
				d.Complete()
				a.dirty = true
			})
		}()
	}
	a.dirty = true
}

func noPruneViewers() []*Viewer { return nil }
