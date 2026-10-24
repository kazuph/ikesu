package runtime

// Profiling builds can observe owned PTYs without changing API or snapshot
// contracts. Normal binaries leave this nil.
var benchPaneMonitor func(*Pane)
