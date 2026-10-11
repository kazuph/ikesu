package runtime

// EventKind classifies pane lifecycle notifications.
type EventKind int

const (
	// EventExited means the pane's process exited.
	EventExited EventKind = iota
	// EventChanged means the pane's screen content changed.
	EventChanged
	// EventStatus means the pane's agent status changed.
	EventStatus
	EventClipboard
)

// Event is a runtime→TUI notification.
type Event struct {
	Pane int
	Kind EventKind
	Data string
}
