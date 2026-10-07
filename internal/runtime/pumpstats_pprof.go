//go:build pprof

package runtime

import "os"

func init() { pumpStatsPath = os.Getenv("GTX_PUMP_STATS") }
