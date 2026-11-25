package endpoint

import "github.com/kazuph/ikesu/internal/worktree"

const (
	PruneWorkspaces = "prune_workspaces"
	PruneScan       = "prune_scan"
	PruneExec       = "prune_exec"
	PruneScanCancel = "prune_scan_cancel"
)

type PruneCandidate struct {
	Workspace  int                    `json:"workspace"`
	Generation string                 `json:"generation"`
	PaneState  string                 `json:"pane_state"`
	Name       string                 `json:"name"`
	Path       string                 `json:"path"`
	Revision   worktree.PruneRevision `json:"revision"`
	Bytes      uint64                 `json:"bytes"`
}

type PruneScanResult struct {
	ServerGeneration string           `json:"server_generation"`
	Candidates       []PruneCandidate `json:"candidates"`
	Protected        map[string]int   `json:"protected"`
}

type PruneExecRequest struct {
	ServerGeneration string           `json:"server_generation"`
	Candidates       []PruneCandidate `json:"candidates"`
}

type PruneOutcome struct {
	Workspace int    `json:"workspace"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Bytes     uint64 `json:"bytes,omitempty"`
}

type PruneExecResult struct {
	Outcomes []PruneOutcome `json:"outcomes"`
}

const (
	PruneDeleted = "deleted"
	PruneSkipped = "skipped"
	PruneFailed  = "failed"
)
