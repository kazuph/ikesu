package server

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == jobRunnerArg {
		code, _ := RunJobRunner()
		os.Exit(code)
	}
	os.Exit(teststate.Run(m.Run))
}
