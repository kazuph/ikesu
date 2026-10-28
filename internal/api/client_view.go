package api

const MethodClientViewList = "client.view.list"

// ClientView describes a terminal's last painted workspace, never session focus.
// Focused is nil until the terminal explicitly reports focus or blur.
type ClientView struct {
	ClientID    int      `json:"client_id"`
	MachineID   string   `json:"machine_id"`
	Session     string   `json:"session"`
	WorkspaceID string   `json:"workspace_id"`
	PaneIDs     []string `json:"pane_ids"`
	Focused     *bool    `json:"focused"`
}
type ClientViewListResult struct {
	Type  string       `json:"type"`
	Views []ClientView `json:"views"`
}
