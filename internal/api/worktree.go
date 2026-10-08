package api

const (
	MethodWorktreeList   = "worktree.list"
	MethodWorktreeCreate = "worktree.create"
	MethodWorktreeOpen   = "worktree.open"
	MethodWorktreeRemove = "worktree.remove"
)

type WorktreeListParams struct {
	Cwd         *string `json:"cwd,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}
type WorktreeCreateParams struct {
	Base        *string `json:"base,omitempty"`
	Branch      *string `json:"branch,omitempty"`
	Cwd         *string `json:"cwd,omitempty"`
	Focus       bool    `json:"focus"`
	Label       *string `json:"label,omitempty"`
	Path        *string `json:"path,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}
type WorktreeOpenParams struct {
	Branch      *string `json:"branch,omitempty"`
	Cwd         *string `json:"cwd,omitempty"`
	Focus       bool    `json:"focus"`
	Label       *string `json:"label,omitempty"`
	Path        *string `json:"path,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}
type WorktreeRemoveParams struct {
	Force       bool   `json:"force"`
	WorkspaceID string `json:"workspace_id"`
}
type WorkspaceWorktreeInfo struct {
	CheckoutPath     string `json:"checkout_path"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
	RepoKey          string `json:"repo_key"`
	RepoName         string `json:"repo_name"`
	RepoRoot         string `json:"repo_root"`
}
type WorktreeInfo struct {
	Branch           *string `json:"branch,omitempty"`
	IsBare           bool    `json:"is_bare"`
	IsDetached       bool    `json:"is_detached"`
	IsPrunable       bool    `json:"is_prunable"`
	IsLinkedWorktree bool    `json:"is_linked_worktree"`
	Label            string  `json:"label"`
	OpenWorkspaceID  *string `json:"open_workspace_id,omitempty"`
	Path             string  `json:"path"`
}
type WorktreeSourceInfo struct {
	CheckoutPathPrefix *string `json:"checkout_path_prefix,omitempty"`
	RepoKey            string  `json:"repo_key"`
	RepoName           string  `json:"repo_name"`
	RepoRoot           string  `json:"repo_root"`
	SourceCheckoutPath string  `json:"source_checkout_path"`
	SourceWorkspaceID  *string `json:"source_workspace_id,omitempty"`
}
type WorktreeListResult struct {
	Type      string             `json:"type"`
	Source    WorktreeSourceInfo `json:"source"`
	Worktrees []WorktreeInfo     `json:"worktrees"`
}
type WorktreeCreatedResult struct {
	Type      string        `json:"type"`
	Workspace WorkspaceInfo `json:"workspace"`
	Tab       TabInfo       `json:"tab"`
	RootPane  PaneInfo      `json:"root_pane"`
	Worktree  WorktreeInfo  `json:"worktree"`
}
type WorktreeOpenedResult struct {
	Type        string        `json:"type"`
	Workspace   WorkspaceInfo `json:"workspace"`
	Tab         TabInfo       `json:"tab"`
	RootPane    PaneInfo      `json:"root_pane"`
	Worktree    WorktreeInfo  `json:"worktree"`
	AlreadyOpen bool          `json:"already_open"`
}
type WorktreeRemovedResult struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	Forced      bool   `json:"forced"`
}
