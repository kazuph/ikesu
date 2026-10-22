package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
	"strings"
	"testing"
)

func TestPaneReceivesActualConfigurationPath(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Cols: 100, Rows: 30})
	pid := firstPaneID(t, dir)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pid, Text: "printf 'CONFIG_READ_PATH=%s\\n' \"$IKESU_CONFIG_PATH\"", Keys: []string{"Enter"}}, nil)
	waitRead(t, dir, pid, "CONFIG_READ_PATH="+config.Path())
	if strings.Contains(config.Path(), "\n") {
		t.Fatal("invalid isolated configuration path")
	}
}
