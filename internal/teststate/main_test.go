package teststate

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	var err error
	originalGoEnvironment, err = readGoEnvironment()
	if err != nil {
		panic(err)
	}
	os.Exit(Run(m.Run))
}
