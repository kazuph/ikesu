package e2e

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 実物の画面で、ペインを右クリック→「Move pane to space…」→スペース→「New tab」と選ぶと、
// ペインが別のスペースの新しいタブへ移り、続けてタブの右クリック→「Move to space…」で元のスペースへ戻せる。
func TestRightClickMovesPaneAndTabBetweenSpaces(t *testing.T) {
	t.Parallel()
	s := start(t)
	shot := func(name, screen string) {
		if os.Getenv("MOVE_MENU_SHOTS") != "" {
			t.Logf("== %s ==\n%s", name, strings.Join(strings.Split(screen, "\n")[:16], "\n"))
		}
	}
	s.waitFor("boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "[new]") })
	nx, ny := s.find("[new]")
	s.click(nx, ny)
	second := regexp.MustCompile(`(?m)^[▌· ]*2 \S`)
	s.waitFor("workspace 2", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, " %2 terminal ") && second.MatchString(tx)
	})
	s.click(3, 5)
	s.waitFor("back on workspace 1", 5*time.Second, func(tx string) bool { return strings.Contains(tx, " %1 terminal ") })

	// スペース1にペインを1つ残す（最後のペインを移すとスペースごと消えるため）。
	s.rightClick(termW/2+14, 3)
	s.waitFor("split menu", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "Split vertical") })
	sx, sy := s.find("Split vertical")
	s.click(sx, sy)
	s.waitFor("two panes", 5*time.Second, func(tx string) bool { return countPanes(tx) >= 2 })

	s.rightClick(30, 3) // pane 1 is the left half
	s.waitFor("pane menu", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "Move pane to space…") })
	fx, fy := s.find("Split vertical") // the follow-up menus open at the same spot
	mx, my := s.find("Move pane to space…")
	s.click(mx, my)
	s.waitFor("space list", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "Split vertical") })
	shot("space list", s.text())
	s.click(fx, fy)
	s.waitFor("tab list", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "New tab") })
	shot("tab list", s.text())
	tx, ty := s.find("New tab")
	s.click(tx, ty)

	paneWorkspace := func() string {
		out, err := s.cli("pane", "get", "1")
		if err != nil {
			t.Fatal(out, err)
		}
		var r struct {
			Result struct {
				Pane struct {
					WorkspaceID string `json:"workspace_id"`
					TabID       string `json:"tab_id"`
				} `json:"pane"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(out, err)
		}
		return r.Result.Pane.WorkspaceID + " " + r.Result.Pane.TabID
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasPrefix(paneWorkspace(), "s2 ") {
		if time.Now().After(deadline) {
			t.Fatal("pane 1 did not move to space 2:", paneWorkspace())
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.waitFor("pane 1 shown in space 2", 5*time.Second, func(tx string) bool { return strings.Contains(tx, " %1 terminal ") })

	// 移したペインのタブ（タブ帯の「2」）を右クリックし、「Move to space…」で元のスペースへ戻す。
	// Columns are cells: the sidebar title holds a wide 🔍.
	tabX, ty := s.find("· 2")
	if ty != 0 {
		t.Fatal("tab 2 not in tab strip:", strings.Split(s.text(), "\n")[0])
	}
	s.rightClick(tabX+2, 0)
	s.waitFor("tab menu", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "Move to space…") })
	shot("tab menu", s.text())
	fx, fy = s.find("New tab")
	mx, my = s.find("Move to space…")
	s.click(mx, my)
	s.waitFor("space list", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "Move to space…") })
	s.click(fx, fy)
	deadline = time.Now().Add(5 * time.Second)
	for !strings.HasPrefix(paneWorkspace(), "s1 ") {
		if time.Now().After(deadline) {
			t.Fatal("tab with pane 1 did not move back to space 1:", paneWorkspace(), "\n", s.text())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
