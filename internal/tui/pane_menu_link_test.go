package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestPaneMenuLinkUsesClickedCellAndWrappedURL(t *testing.T) {
	a := specRuntimeApp(t)
	first := a.focusedPane()
	second := first + 1
	a.st.ActiveWorkspace().TabRef().Layout.SplitFocused(layout.Vertical, second)
	firstRect, secondRect := a.innerRect(first), a.innerRect(second)

	other := "https://other.test/"
	short := "https://one.test/"
	long := "https://two.test/oauth?scope=" + strings.Repeat("a%3Ab+", 13) + "c&redirect_uri=https%3A%2F%2Freturn.test"
	const osc = "https://label.test/path"
	unsafe := "file:///etc/passwd"
	firstPane, err := a.sup.SpawnWith(first, a.cwd, firstRect.W, firstRect.H, nil, []string{"/bin/sh", "-c", `printf '%s\r\n' "$1"; exec /bin/cat`, "test", other})
	if err != nil {
		t.Fatal(err)
	}
	secondPane, err := a.sup.SpawnWith(second, a.cwd, secondRect.W, secondRect.H, nil, []string{"/bin/sh", "-c",
		`printf '界 %s %s\r\n\033]8;;%s\033\\表示\033]8;;\033\\  \033]8;;%s\033\\unsafe\033]8;;\033\\\r\nREADY' "$1" "$2" "$3" "$4"; exec /bin/cat`, "test", short, long, osc, unsafe})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, firstPane, other)
	specWaitText(t, secondPane, "READY")

	var output bytes.Buffer
	v := a.NewViewer(&output, a.st.Cols, a.st.Rows, nil)
	var opened []string
	v.SetLinkOpener(func(url string) error {
		opened = append(opened, url)
		return nil
	})
	rightClick := func(x, y int) {
		t.Helper()
		v.Input(a, []byte(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x+1, y+1, x+1, y+1)))
		if v.app.menu == nil {
			t.Fatal("right click did not open the pane menu", x, y)
		}
	}
	pick := func(downs int) {
		t.Helper()
		v.Input(a, []byte(strings.Repeat("\x1b[B", downs)+"\r"))
		if v.app.menu != nil {
			t.Fatal("activation left the menu open")
		}
	}
	assertLink := func(want string) {
		t.Helper()
		if m := v.app.menu; m.link != want {
			t.Fatalf("clicked link = %q, want %q", m.link, want)
		}
	}

	rightClick(secondRect.X+1, secondRect.Y+1)
	assertLink(long)
	var labels []string
	for _, item := range v.app.menu.m.Items[:8] {
		label := item.Label
		if item.Separator {
			label = "--"
		}
		labels = append(labels, label)
	}
	want := []string{"Split vertical", "Split horizontal", "--", "Copy Link", "Open Link", "--", "Copy handoff snapshot", "Rename pane"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatal(labels)
	}
	pick(2)
	sequence := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(long)) + "\a"
	if !bytes.Contains(output.Bytes(), []byte(sequence)) {
		t.Fatal("Copy Link did not emit the full clicked URL")
	}
	rightClick(secondRect.X+4, secondRect.Y)
	assertLink(short)
	pick(3)
	rightClick(firstRect.X+1, firstRect.Y)
	assertLink(other)
	pick(3)
	linkRows := (3 + len(short) + 1 + len(long) + secondRect.W - 1) / secondRect.W
	rightClick(secondRect.X+1, secondRect.Y+linkRows)
	assertLink(osc)
	pick(3)
	if !reflect.DeepEqual(opened, []string{short, other, osc}) {
		t.Fatal("Open Link used the wrong URL", opened)
	}
	rightClick(secondRect.X+7, secondRect.Y+linkRows)
	assertLink("")
	if menuIndex(v.app, "Open Link") >= 0 || menuIndex(v.app, "Copy Link") >= 0 {
		t.Fatal("unsafe OSC 8 scheme offered as a web link")
	}
}

func TestPaneMenuLinkUsesParkedCopyViewBeforeFocus(t *testing.T) {
	a := specRuntimeApp(t)
	first := a.focusedPane()
	a.st.ActiveWorkspace().TabRef().Layout.SplitFocused(layout.Vertical, first+1)
	r := a.innerRect(first)
	const old = "https://history.test/"
	const live = "https://live.test/"
	p, err := a.sup.SpawnWith(first, a.cwd, r.W, r.H, nil, []string{"/bin/sh", "-c",
		`printf '%s\r\n' "$1"; i=1; while [ "$i" -lt "$3" ]; do printf '\r\n'; i=$((i+1)); done
		  printf '%s\r\n' "$2"; i=2; while [ "$i" -lt "$3" ]; do printf '\r\n'; i=$((i+1)); done
		  printf 'READY'; exec /bin/cat`, "test", old, live, fmt.Sprint(r.H)})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	if current := p.WebLinkAt(1, 0, 0, r.H, true); current != live {
		t.Fatal("expected a different live URL at the click position", current)
	}
	var out bytes.Buffer
	v := a.NewViewer(&out, a.st.Cols, a.st.Rows, nil)
	v.app.paneCopies = map[int]*copyMode{first: {pane: first, top: 0}}
	x, y := r.X+2, r.Y+1
	v.Input(a, []byte(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", x, y, x, y)))
	if v.app.menu == nil || v.app.menu.link != old || v.app.cm == nil || v.app.cm.pane != first {
		t.Fatal("menu did not use the parked copy viewport before restoring it")
	}
}
