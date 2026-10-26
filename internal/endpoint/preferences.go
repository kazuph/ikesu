package endpoint

// ViewPreferences are saved by the attached client, never by its server.
type ViewPreferences struct {
	SidebarWidth        int      `json:"sidebar_width"`
	SidebarPresetActive bool     `json:"sidebar_preset_active,omitempty"`
	SidebarManual       bool     `json:"sidebar_manual"`
	SidebarHidden       bool     `json:"sidebar_hidden,omitempty"`
	RightSidebarManual  bool     `json:"right_sidebar_manual,omitempty"`
	RightSidebarWidth   int      `json:"right_sidebar_width,omitempty"`
	RightSidebarMode    string   `json:"right_sidebar_mode,omitempty"`
	SidebarCollapsed    bool     `json:"sidebar_collapsed"`
	SidebarSectionSplit *float64 `json:"sidebar_section_split,omitempty"`
}
