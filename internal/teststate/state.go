// Package teststate serializes tests that change process-wide state.
package teststate

import (
	"os"
	"sync"
	"testing"
)

var processState sync.Mutex

func Lock(t *testing.T) {
	t.Helper()
	processState.Lock()
	t.Cleanup(processState.Unlock)
}

func Setenv(t *testing.T, key, value string) {
	t.Helper()
	old, present := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		if present {
			err = os.Setenv(key, old)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Error(err)
		}
	})
}
