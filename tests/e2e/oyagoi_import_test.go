package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Starting a real terminal and importing a session can exceed five seconds.
func TestOyagoiRemainsSingleAfterImport(t *testing.T) {
	s := start(t)
	s.waitFor("initial shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	if out, err := s.cli("workspace", "rename", "s1", "HQ-original"); err != nil {
		t.Fatalf("rename workspace: %s %v", out, err)
	}
	s.waitFor("renamed workspace", 15*time.Second, func(text string) bool { return strings.Contains(text, "HQ-original") })
	x, y := s.find("HQ-original")
	s.click(x, y)
	s.waitFor("HQ menu", 15*time.Second, func(text string) bool { return strings.Contains(text, "set as HQ") })
	x, y = s.find("set as HQ")
	s.click(x, y)
	s.waitFor("HQ selected", 15*time.Second, func(text string) bool {
		return strings.Contains(text, "🎏 oyagoi") && strings.Contains(text, "HQ-original")
	})
	source := t.TempDir()
	data := fmt.Sprintf(`{"version":5,"active":0,"selected":0,"sidebar_width":27,"workspaces":[{"id":"s1","custom_name":"Imported-room","section":"work","identity_cwd":%q,"active_tab":0,"public_tab_numbers":[1],"next_public_tab_number":2,"tabs":[{"custom_name":"shell","layout":{"Pane":1},"pane_order":[1],"focused":1,"root_pane":1,"panes":{"1":{"global_pane_number":1,"cwd":%q}}}]}]}`, source, source)
	if err := os.WriteFile(filepath.Join(source, "session.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("session", "import-herdr", "--source", source, "--yes"); err != nil {
		t.Fatalf("import: %s %v", out, err)
	}
	s.waitFor("imported workspace and single HQ", 15*time.Second, func(text string) bool {
		return strings.Count(text, "🎏 oyagoi") == 1 && strings.Count(text, "HQ-original") == 1 && strings.Count(text, "Imported-room") == 1
	})
	_, headingY := s.find("🎏 oyagoi")
	_, originalY := s.find("HQ-original")
	_, importedY := s.find("Imported-room")
	if !(headingY < originalY && originalY < importedY) {
		t.Fatalf("unexpected HQ ordering: heading=%d original=%d imported=%d\n%s", headingY, originalY, importedY, s.text())
	}
}
