package api

type PaneReportMetadataParams struct {
	PaneID            string             `json:"pane_id"`
	Source            string             `json:"source"`
	Agent             *string            `json:"agent,omitempty"`
	AppliesToSource   *string            `json:"applies_to_source,omitempty"`
	Title             *string            `json:"title,omitempty"`
	ClearTitle        bool               `json:"clear_title,omitempty"`
	DisplayAgent      *string            `json:"display_agent,omitempty"`
	ClearDisplayAgent bool               `json:"clear_display_agent,omitempty"`
	StateLabels       map[string]string  `json:"state_labels,omitempty"`
	ClearStateLabels  bool               `json:"clear_state_labels,omitempty"`
	Tokens            map[string]*string `json:"tokens,omitempty"`
	Seq               *uint64            `json:"seq,omitempty"`
	TTLms             *uint64            `json:"ttl_ms,omitempty"`
}

type PaneMetadataView struct {
	Title, DisplayAgent string
	StateLabels, Tokens map[string]string
}
