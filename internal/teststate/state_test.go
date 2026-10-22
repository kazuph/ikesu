package teststate

import (
	"fmt"
	"os"
	"testing"
)

func TestG9ParallelStateLockAndExplicitRestore(t *testing.T) {
	const key = "IKESU_TEST_PROCESS_STATE"
	old, present := os.LookupEnv(key)
	t.Run("parallel", func(t *testing.T) {
		for i := 0; i < 16; i++ {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				Lock(t)
				got, exists := os.LookupEnv(key)
				if got != old || exists != present {
					t.Fatal("prior test did not restore process state")
				}
				value := t.Name()
				Setenv(t, key, value)
				if os.Getenv(key) != value {
					t.Fatal("another test changed locked state")
				}
			})
		}
	})
	if got, exists := os.LookupEnv(key); got != old || exists != present {
		t.Fatal("parallel tests did not restore process state")
	}
}
