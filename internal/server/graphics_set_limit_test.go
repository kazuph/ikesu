package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestGraphicsSetDecodedLimitIsIndependentOfFileAndStreamBudgets(t *testing.T) {
	teststate.Lock(t)
	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_CONFIG", config)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	pane := firstPaneID(t, dir)
	for _, size := range []int{api.MaxGraphicsSetBytes - 4, api.MaxGraphicsSetBytes, api.MaxGraphicsSetBytes + 4} {
		response := apiReq(t, dir, api.MethodPaneGraphicsSet, map[string]any{
			"pane_id": pane, "format": "rgba", "image_width": size / 4, "image_height": 1,
			"data_base64": base64.StdEncoding.EncodeToString(make([]byte, size)),
			"placement":   map[string]any{"grid_cols": 1, "grid_rows": 1},
		})
		if size <= api.MaxGraphicsSetBytes {
			if response.Err != nil {
				t.Fatal("valid set boundary rejected", size, response.Err)
			}
		} else if response.Err == nil || response.Err.Code != "image_too_large" || response.Err.Message != "image data is too large" {
			t.Fatal("set overflow did not match the observed response", size, response)
		}
	}
}
