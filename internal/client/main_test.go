package client

import (
	"os"
	"testing"

	"github.com/kazuph/goterrace/internal/server"
)

// The right-sidebar harness runs a real server in this test executable.
// Its detached job children need the same private entry point as cmd/gtx.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__job-runner" {
		code, _ := server.RunJobRunner()
		os.Exit(code)
	}
	os.Exit(m.Run())
}
