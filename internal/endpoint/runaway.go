package endpoint

const Runaway = "runaway_action"
const RunawayAlerts = "runaway_alerts"

type RunawayAlert struct {
	Machine            string  `json:"machine"`
	ID                 string  `json:"id"`
	Pane               int     `json:"pane"`
	Workspace          int     `json:"workspace"`
	SpaceName          string  `json:"space_name"`
	PID                int     `json:"pid"`
	Name               string  `json:"name"`
	Command            string  `json:"command"`
	RSS                int64   `json:"rss_kib"`
	TreeRSS            int64   `json:"tree_rss_kib"`
	TreeSpike          int64   `json:"tree_spike_kib"`
	Spike              int64   `json:"spike_kib"`
	StopDisabledReason string  `json:"stop_disabled_reason,omitempty"`
	CPUPercent         float64 `json:"cpu_percent"`
	CPUSeconds         int64   `json:"cpu_seconds"`
}

type RunawayAction struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

const (
	RunawayInspect = "inspect"
	RunawayTerm    = "term"
	RunawayKill    = "kill"
	RunawayFocus   = "focus"
	RunawaySnooze  = "snooze"
)
