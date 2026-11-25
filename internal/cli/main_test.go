package cli

import (
	"fmt"
	"os"
	"testing"

	"github.com/kazuph/ikesu/internal/server"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMain(m *testing.M) {
	if args := os.Args[1:]; len(args) >= 2 && args[0] == "--snapshot-cli-test-child" {
		args = args[1:]
		// Exercise the normal CLI with the real environment inherited from
		// the pane. Test isolation in the parent already protects live state.
		if args[0] == "server" {
			code, err := server.Run(args[1], "snapshot-test")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(code)
		}
		os.Exit(Run(args, Options{}))
	}
	os.Exit(teststate.Run(m.Run))
}
