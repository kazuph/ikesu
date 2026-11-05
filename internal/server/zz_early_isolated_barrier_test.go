package server

import "testing"

// This final serial test prevents early children from overlapping Go's
// parallel phase; filtered and shuffled runs use the ordinary parallel path.
func TestZZZWaitForEarlyIsolated(t *testing.T) {
	earlyIsolatedWait.Wait()
}
