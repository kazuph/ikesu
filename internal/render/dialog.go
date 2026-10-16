package render

// Dialog is a centered confirmation box.
type Dialog struct {
	WorktreeCreate bool
	Checkout       string
	ContinueOnly   bool
	Title          string
	Detail         string
	Confirm        string // e.g. "quit"
	Cancel         string // e.g. "cancel"
	// Lines are extra detail rows below Detail (e.g. the missing-session
	// warning list); at most 10 are drawn, per SPEC's dialog height rule
	// 5 + min(count, 10).
	Lines []string
	// Width pins the dialog width (0 → auto-fit contents).
	Width int
	// Workspace-close confirmation dims the panes while retaining the surrounding controls.
	DimPanesOnly     bool
	CenterTerminal   bool
	CompactButtons   bool
	BoldDetailPrefix bool
	// Input/InputEdit render the detail row as an editable text field.
	Input             string
	InputCursor       int // rune index in Input
	InputEdit         bool
	PreserveTitleCase bool
}
