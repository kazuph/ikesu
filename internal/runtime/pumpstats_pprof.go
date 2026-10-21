//go:build pprof

package runtime

import "github.com/kazuph/ikesu/internal/envcompat"

func init() { pumpStatsPath = envcompat.Get("GTX_PUMP_STATS") }
