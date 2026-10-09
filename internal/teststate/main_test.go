package teststate

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(Run(m.Run)) }
