package layout

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(teststate.Run(m.Run)) }
