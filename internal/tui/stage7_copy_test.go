package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func stage7CopyApp(t *testing.T) *App {
	t.Helper()
	a := specRuntimeApp(t)
	a.sidebarConfig.Keys.Prefix = "ctrl+t"
	inner := a.innerRect(a.focusedPane())
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, inner.W, inner.H, nil,
		[]string{"/bin/sh", "-c", "i=1; while [ $i -le 120 ]; do printf 'ROW-%03d alpha ALPHA Beta\\n' $i; i=$((i+1)); done; printf 'STAGE7_READY\\n'; exec /bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "STAGE7_READY")
	a.enterCopy(false)
	return a
}

func TestStage7ConfiguredPrefixCopyPages(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	before := *a.cm
	step := a.copyInnerH() - copyPageOverlapRows
	a.handleKey(uv.Key{Code: 'b', Mod: uv.ModCtrl}, nil)
	if a.prefix || a.cm.top != before.top-step || a.cm.cy != before.cy-step {
		t.Fatalf("Ctrl+b: prefix=%v copy=%+v before=%+v", a.prefix, a.cm, before)
	}
	a.handleKey(uv.Key{Code: 'f', Mod: uv.ModCtrl}, nil)
	if a.cm.top != before.top || a.cm.cy != before.cy {
		t.Fatalf("Ctrl+f did not restore page: %+v", a.cm)
	}
	a.handleKey(uv.Key{Code: 't', Mod: uv.ModCtrl}, nil)
	if !a.prefix {
		t.Fatal("configured Ctrl+t prefix unavailable")
	}
}

func TestStage7LineSelection(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	a.copyKey(uv.Key{Code: 'g'}, nil)
	a.cm.cx = 5
	a.copyKey(uv.Key{Code: 'V'}, nil)
	a.copyKey(uv.Key{Code: 'j'}, nil)
	from, to, ok := a.copySelRange()
	if !ok || from.x != 0 || to.x != a.copyInnerW()-1 || from.y != 0 || to.y != 1 {
		t.Fatalf("line selection: %v %v %v", from, to, ok)
	}
	text := a.copyText(a.sup.Get(a.cm.pane), from, to)
	if text != "ROW-001 alpha ALPHA Beta\nROW-002 alpha ALPHA Beta" {
		t.Fatalf("whole rows: %q", text)
	}
}

func TestStage7EnterCopyPreservesScroll(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	pid := a.cm.pane
	a.exitCopy()
	a.scroll[pid] = 24
	before := viewTop(a.sup.Get(pid), 24, a.innerRect(pid).H)
	a.enterCopy(false)
	if a.cm.top != before || a.cm.cy != before+a.copyInnerH()-1 || a.cm.cx != 0 {
		t.Fatalf("copy view=%+v want top=%d bottom cursor", a.cm, before)
	}
	a.exitCopy()
	if a.scroll[pid] != 24 {
		t.Fatal("copy changed normal scroll")
	}
}

func TestStage7SmartSearchAndCancellation(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	a.cm.query = "alpha"
	a.copyFindAll()
	if len(a.cm.matches) != 240 {
		t.Fatalf("smart lowercase count=%d", len(a.cm.matches))
	}
	a.cm.query = "Alpha"
	a.copyFindAll()
	if len(a.cm.matches) != 0 {
		t.Fatal("uppercase query must be case sensitive")
	}
	a.cm.query = "alpha"
	a.copyFindAll()
	a.copyNextMatch(1)
	before := *a.cm
	specFeed(a, "/Beta")
	a.copySearchKey(uv.Key{Code: uv.KeyEscape})
	if a.cm.query != "alpha" || len(a.cm.matches) != 240 || a.cm.matchIdx != before.matchIdx {
		t.Fatalf("cancel lost confirmed search: %+v", a.cm)
	}
	specFeed(a, "/")
	a.copySearchKey(uv.Key{Code: uv.KeyEnter})
	if a.cm.query != "" || len(a.cm.matches) != 240 || a.cm.matchIdx != before.matchIdx {
		t.Fatalf("empty confirmation replaced matches: %+v", a.cm)
	}
	specFeed(a, "/Beta")
	a.copySearchKey(uv.Key{Code: 'u', Mod: uv.ModCtrl})
	if a.cm.query != "" || !a.cm.searching {
		t.Fatal("Ctrl+u did not clear input")
	}
}

func TestStage7PaneReturnRestoresSearchAndViewport(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	first := a.focusedPane()
	a.exitCopy()
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	other := a.focusedPane()
	a.exec(model.CmdFocusPane{Pane: first})
	a.enterCopy(false)
	specFeed(a, "/alpha\r")
	specFeed(a, "vkk")
	before := *a.cm
	a.exec(model.CmdFocusPane{Pane: other})
	if a.cm != nil {
		t.Fatal("copy view remained on other pane")
	}
	a.exec(model.CmdFocusPane{Pane: first})
	if a.cm == nil {
		t.Fatal("return lost copy state")
	}
	if a.cm.query != before.query || a.cm.top != before.top || a.cm.cx != before.cx || a.cm.cy != before.cy || a.cm.matchIdx != before.matchIdx {
		t.Fatalf("return changed search/cursor: before=%+v after=%+v", before, a.cm)
	}
	if a.cm.anchor != nil {
		t.Fatal("real reference clears selection on departure")
	}
}
