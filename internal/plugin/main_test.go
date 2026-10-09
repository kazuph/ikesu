package plugin

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(teststate.Run(m.Run)) }
