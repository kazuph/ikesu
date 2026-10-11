package endpoint

// ViewPreferences are saved by the attached client, never by its server.
type ViewPreferences struct {
	SidebarWidth        int      `json:"sidebar_width"`
	SidebarManual       bool     `json:"sidebar_manual"`
	SidebarCollapsed    bool     `json:"sidebar_collapsed"`
	SidebarSectionSplit *float64 `json:"sidebar_section_split,omitempty"`
}
