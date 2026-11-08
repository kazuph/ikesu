package endpoint

// IdlePane identifies the exact pane state on which a deletion decision is based.
type IdlePane struct {
	ID                  int    `json:"id"`
	Generation          string `json:"generation"`
	ActivitySeq         uint64 `json:"activity_seq"`
	StateSeq            uint64 `json:"state_seq"`
	State               string `json:"state"`
	Agent               string `json:"agent"`
	Workspace           int    `json:"workspace"`
	WorkspaceGeneration string `json:"workspace_generation"`
	WorkspaceName       string `json:"workspace_name"`
	TabName             string `json:"tab_name"`
	AgeNanos            int64  `json:"age_nanos"`
	PendingInput        bool   `json:"pending_input"`
	InputKnown          bool   `json:"input_known"`
	Focused             bool   `json:"focused"`
}

type IdlePanes struct {
	ServerGeneration string     `json:"server_generation"`
	Panes            []IdlePane `json:"panes"`
	LastPaneKept     bool       `json:"last_pane_kept"`
}

type IdleDelete struct {
	ServerGeneration string     `json:"server_generation"`
	Panes            []IdlePane `json:"panes"`
}

type IdleDeleteResult struct {
	Deleted int `json:"deleted"`
	Failed  int `json:"failed"`
}
