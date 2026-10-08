package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"testing"
)

func TestSPECCopySearchWordPageAndLineEndActualBuffer(t *testing.T) {
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	p, err := a.sup.SpawnWith(pid, a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 100 ]; do printf 'alpha beta gamma %s\\r\\n' $i; i=$((i+1)); done; printf DONE; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "DONE")
	a.enterCopy(false)
	key := func(code rune) { a.copyKey(uv.Key{Code: code}, nil) }
	key('g')
	if a.cm.cy != 0 || a.cm.cx != 0 {
		t.Fatal("start", a.cm)
	}
	key('w')
	if a.cm.cx != 6 || a.cm.cy != 0 {
		t.Fatal("word start", a.cm)
	}
	key('e')
	if a.cm.cx != 9 {
		t.Fatal("word end", a.cm)
	}
	key('b')
	if a.cm.cx != 6 {
		t.Fatal("word back", a.cm)
	}
	key('$')
	end := a.cm.cx
	for i := 0; i < 100; i++ {
		key('l')
	}
	if a.cm.cx != end {
		t.Fatal("passed text line end")
	}
	h := a.copyInnerH()
	key(uv.KeyPgDown)
	if a.cm.cy != h {
		t.Fatal("page down", a.cm.cy, h)
	}
	key(uv.KeyPgUp)
	if a.cm.cy != 0 {
		t.Fatal("page up", a.cm.cy)
	}
	key('/')
	for _, r := range "beta" {
		a.copyKey(uv.Key{Code: r, Text: string(r)}, nil)
	}
	key(uv.KeyEnter)
	if a.cm.cy != 1 || a.cm.cx != 6 {
		t.Fatal("search forward", a.cm)
	}
	key('n')
	if a.cm.cy != 2 || a.cm.cx != 6 {
		t.Fatal("repeat forward", a.cm)
	}
	key('N')
	if a.cm.cy != 1 || a.cm.cx != 6 {
		t.Fatal("repeat reverse", a.cm)
	}
}
