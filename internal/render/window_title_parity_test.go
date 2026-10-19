package render

import "testing"

func TestParityPlainRetainsNamedSession(t *testing.T) {
	in := titleInput("working")
	in.SidebarConfig.UI.WindowTitle = "plain"
	if got := WindowTitle(in, "project", nil); got != "gtx:project" {
		t.Fatal(got)
	}
}
