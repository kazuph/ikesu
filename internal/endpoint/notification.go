package endpoint

// Notification is delivered to the attached host; no OS action runs on the
// session server, including sessions reached through a saved machine.
type Notification struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	Kind         string `json:"kind"`
	Agent        string `json:"agent,omitempty"`
	TargetPaneID string `json:"target_pane_id"`
	SystemToast  bool   `json:"system_toast"`
}
