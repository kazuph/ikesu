package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkHandlersOrderContextAndDisabledRealCommand(t *testing.T) {
	root := t.TempDir()
	s := &Store{ConfigRoot: t.TempDir(), StateRoot: t.TempDir(), Version: "0.1.0"}
	manifest := `id="link.test"
name="Link"
version="0.1.0"
min_ikesu_version="0.1.0"
[[actions]]
id="capture"
title="Capture"
command=["/bin/sh","-c","printf '%s' \"$IKESU_PLUGIN_CONTEXT_JSON\""]
[[link_handlers]]
id="first"
title="First"
pattern="^https://example[.]test/"
action="capture"
[[link_handlers]]
id="second"
title="Second"
pattern="example"
action="capture"
`
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(root, true, nil); err != nil {
		t.Fatal(err)
	}
	e := Engine{Store: s, BinPath: "/ikesu", SocketPath: "/socket"}
	found, err := e.InvokeLink("https://example.test/a", Context{"workspace_id": "s1"})
	if !found || err != nil {
		t.Fatal(found, err)
	}
	e.Wait()
	logs, err := s.Logs("link.test", nil)
	if err != nil || len(logs) != 1 || logs[0].Stdout == nil {
		t.Fatal(logs, err)
	}
	var context Context
	if err := json.Unmarshal([]byte(*logs[0].Stdout), &context); err != nil {
		t.Fatal(err)
	}
	if context["link_handler_id"] != "first" || context["clicked_url"] != "https://example.test/a" || context["invocation_source"] != "link_click" || context["workspace_id"] != "s1" {
		t.Fatal(context)
	}
	if _, err := s.SetEnabled("link.test", false); err != nil {
		t.Fatal(err)
	}
	if found, err = e.InvokeLink("https://example.test/a", nil); found || err != nil {
		t.Fatal(found, err)
	}
}
