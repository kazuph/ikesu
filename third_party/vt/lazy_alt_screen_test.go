package vt

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"testing"
)

func TestAlternateScreenAllocatedOnFirstUse(t *testing.T) {
	e := NewEmulator(80, 24)
	if e.scrs[1].buf != nil {
		t.Fatal("alternate screen allocated at construction")
	}
	_, _ = e.Write([]byte("hello"))
	if e.scrs[1].buf != nil {
		t.Fatal("alternate screen allocated by main screen output")
	}
}

func TestLazyAlternateResizeResetAndHandoff(t *testing.T) {
	for _, reflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "resize", true: "reflow"}[reflow], func(t *testing.T) {
			e := NewEmulator(80, 24)
			defer e.Close()
			if reflow {
				e.ResizeWithReflow(40, 12, nil)
			} else {
				e.Resize(40, 12)
			}
			_, _ = e.WriteString("main\x1bc")
			if e.scrs[1].buf != nil {
				t.Fatal("resize or reset allocated alternate screen")
			}
			data, err := e.MarshalHandoff()
			if err != nil {
				t.Fatal(err)
			}
			if e.scrs[1].buf != nil {
				t.Fatal("export retained alternate screen")
			}
			restored := NewEmulator(1, 1)
			defer restored.Close()
			if err := restored.UnmarshalHandoff(data); err != nil {
				t.Fatal(err)
			}
			if restored.scrs[1].buf != nil {
				t.Fatal("import retained unused alternate screen")
			}
			_, _ = restored.WriteString("\x1b[?1049h")
			if !restored.IsAltScreen() || restored.Width() != 40 || restored.Height() != 12 {
				t.Fatalf("alternate dimensions or mode: %dx%d alt=%v", restored.Width(), restored.Height(), restored.IsAltScreen())
			}
		})
	}
}

func TestLazyAlternateHandoffUsedScreen(t *testing.T) {
	e := NewEmulator(40, 12)
	defer e.Close()
	_, _ = e.WriteString("main\x1b[?1049hALT\x1b7\x1b[3;5H")
	data, err := e.MarshalHandoff()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewEmulator(1, 1)
	defer restored.Close()
	if err = restored.UnmarshalHandoff(data); err != nil {
		t.Fatal(err)
	}
	if !restored.IsAltScreen() || restored.Render() != e.Render() || restored.CursorPosition() != e.CursorPosition() || restored.scrs[1].saved != e.scrs[1].saved {
		t.Fatal("alternate screen state changed during handoff")
	}
	_, _ = restored.WriteString("\x1b[?1049l")
	if restored.IsAltScreen() {
		t.Fatal("did not return to main screen")
	}
}

// The eagerly initialized screen reproduces the old storage lifecycle.
func TestLazyAlternateMatchesEagerScreen(t *testing.T) {
	for _, mode := range []int{47, 1047, 1049} {
		for _, reflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("mode%d_reflow%v", mode, reflow), func(t *testing.T) {
				lazy, eager := NewEmulator(18, 6), NewEmulator(18, 6)
				defer lazy.Close()
				defer eager.Close()
				eager.scrs[1] = *NewScreen(18, 6)
				eager.scrs[1].cb = &eager.cb
				for step := 0; step < 8; step++ {
					for _, e := range []*Emulator{lazy, eager} {
						switch step {
						case 0:
							_, _ = e.WriteString("main\r\ncontent\x1b[3;4H\x1b7")
						case 1:
							if reflow {
								e.ResizeWithReflow(13, 4, nil)
							} else {
								e.Resize(13, 4)
							}
						case 2:
							_, _ = e.WriteString(fmt.Sprintf("\x1b[?%dhALT\x1b[2;3H\x1b7", mode))
						case 3:
							if reflow {
								e.ResizeWithReflow(10, 5, nil)
							} else {
								e.Resize(10, 5)
							}
						case 4:
							_, _ = e.WriteString(fmt.Sprintf("\x1b[?%dl", mode))
						case 5:
							_, _ = e.WriteString("\x1bc")
						case 6:
							if reflow {
								e.ResizeWithReflow(15, 7, nil)
							} else {
								e.Resize(15, 7)
							}
						case 7:
							_, _ = e.WriteString(fmt.Sprintf("\x1b[?%dh\x1b8", mode))
						}
					}
					state := func(e *Emulator) terminalTransfer {
						b, err := e.MarshalHandoff()
						if err != nil {
							t.Fatal(err)
						}
						var v terminalTransfer
						if err = gob.NewDecoder(bytes.NewReader(b)).Decode(&v); err != nil {
							t.Fatal(err)
						}
						return v
					}
					a, b := state(lazy), state(eager)
					if !reflect.DeepEqual(a, b) || lazy.IsAltScreen() != eager.IsAltScreen() {
						t.Fatalf("step %d differs between lazy and eager", step)
					}
				}
			})
		}
	}
}
