package runtime

// pumpStatsPath, when non-empty, is where readPTY writes its read/chunk
// counters. Only profiling builds (-tags pprof) set it, from GTX_PUMP_STATS.
var pumpStatsPath string
