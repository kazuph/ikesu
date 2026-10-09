package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func hasTabLabel(screen, label string) bool {
	row := strings.Split(screen, "\n")[0] + " "
	return strings.Contains(row, " "+label+" ")
}

func (s *session) findTab(label string) (int, int) {
	row := strings.Split(s.text(), "\n")[0] + " "
	i := strings.Index(row[26:], " "+label+" ")
	if i < 0 {
		return -1, -1
	}
	return 26 + i + 1, 0
}

func TestTabBar(t *testing.T) {
	s := startLocal(t)

	s.waitFor("boot", 10*time.Second, func(scr string) bool {
		return hasTabLabel(scr, "1") && strings.Contains(scr, "❯")
	})

	// C-b c creates a focused second tab; the strip shows both.
	s.send("\x02c")
	s.waitFor("new tab name prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "new tab") && strings.Contains(tx, "↵ save") })
	s.send("\r")
	s.waitFor("second tab", 5*time.Second, func(scr string) bool {
		return hasTabLabel(scr, "2")
	})
	// New tab has exactly one pane while tab 1 keeps its own layout.
	if n := countPanes(s.text()); n != 1 {
		t.Fatalf("pane count in new tab = %d, want 1; screen:\n%s", n, s.text())
	}

	// Split inside tab 2; its layout is independent of tab 1.
	s.send("\x02v")
	s.waitFor("split in tab 2", 5*time.Second, func(scr string) bool {
		return countPanes(scr) == 2
	})

	// Clicking tab 1's cell switches back (prefix+[ / ] now enter copy
	// mode per SPEC G6; tab cells are the switch surface).
	x, y := s.findTab("1")
	if x < 0 {
		t.Fatalf("tab 1 cell not found; screen:\n%s", s.text())
	}
	s.click(x, y)
	s.waitFor("tab 1 layout", 5*time.Second, func(scr string) bool {
		return countPanes(scr) == 1
	})
	x, y = s.findTab("2")
	s.click(x, y)
	s.waitFor("tab 2 layout", 5*time.Second, func(scr string) bool {
		return countPanes(scr) == 2
	})

	// Mouse: clicking the tab 1 cell focuses it.
	x, y = s.findTab("1")
	if x < 0 {
		t.Fatalf("tab 1 cell not found; screen:\n%s", s.text())
	}
	s.click(x, y)
	s.waitFor("click focuses tab 1", 5*time.Second, func(scr string) bool {
		return countPanes(scr) == 1
	})

	// The distribution ignores middle-click, preserving both tabs.
	x, y = s.findTab("2")
	if x < 0 {
		t.Fatalf("tab 2 cell not found; screen:\n%s", s.text())
	}
	s.send(fmt.Sprintf("\x1b[<1;%d;%dM\x1b[<1;%d;%dm", x+1, y+1, x+1, y+1))
	s.pump(time.Now().Add(300 * time.Millisecond))
	if !hasTabLabel(s.text(), "1") || !hasTabLabel(s.text(), "2") || countPanes(s.text()) != 1 {
		t.Fatal("middle-click changed tabs", s.text())
	}

	// Mouse: the "+" button creates a tab.
	x, y = s.find("+")
	if x < 0 {
		t.Fatalf("+ button not found; screen:\n%s", s.text())
	}
	s.click(x, y)
	s.waitFor("new tab name prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "new tab") && strings.Contains(tx, "↵ save") })
	s.send("\r")
	s.waitFor("plus creates tab", 5*time.Second, func(scr string) bool {
		return hasTabLabel(scr, "3")
	})
}

func TestTabRenameDialog(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 10*time.Second, func(scr string) bool {
		return hasTabLabel(scr, "1")
	})

	// Right-click the tab cell → menu → "Rename tab…" opens the input.
	x, y := s.findTab("1")
	if x < 0 {
		t.Fatalf("tab 1 cell not found; screen:\n%s", s.text())
	}
	s.rightClick(x, y)
	s.waitFor("tab menu", 5*time.Second, func(scr string) bool {
		return strings.Contains(scr, "Rename tab")
	})
	s.send("\r") // activate: Rename tab…
	s.waitFor("rename dialog", 5*time.Second, func(scr string) bool {
		return strings.Contains(scr, "rename tab") && strings.Contains(scr, "^c clear")
	})
	s.send("\x03prod")
	s.send("\r")
	s.waitFor("renamed", 5*time.Second, func(scr string) bool {
		return strings.Contains(scr, "prod")
	})
}

func TestTabRightClickPreservesSelection(t *testing.T) {
	s := start(t)
	s.waitFor("boot", 15*time.Second, func(tx string) bool {
		return hasTabLabel(tx, "1") && strings.Contains(tx, "❯")
	})
	// Tab 2 with a split so the active tab is identifiable by pane count.
	s.send("\x02c")
	s.waitFor("new tab name prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "new tab") && strings.Contains(tx, "↵ save") })
	s.send("\r")
	s.waitFor("tab 2", 5*time.Second, func(tx string) bool { return hasTabLabel(tx, "2") })
	s.send("\x02v")
	s.waitFor("tab 2 split", 5*time.Second, func(tx string) bool { return countPanes(tx) == 2 })
	x, y := s.findTab("1")
	if x < 0 {
		t.Fatalf("tab 1 not found:\n%s", s.text())
	}
	s.rightClick(x, y)
	s.waitFor("tab menu", 5*time.Second, func(tx string) bool {
		return strings.Contains(tx, "Rename tab")
	})
	// Closing the menu reveals that tab 2 retains its split layout.
	s.send("\x1b")
	s.waitFor("tab 2 remains selected", 5*time.Second, func(tx string) bool {
		return countPanes(tx) == 2
	})
}
