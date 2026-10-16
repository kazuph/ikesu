package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
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
