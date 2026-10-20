package tui

import (
	"bytes"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"path/filepath"
	"testing"
	"time"
)

func TestLeftSidebarDividerSameCellMotionRemainsClick(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, offset := range []int{-1, 0} {
			for _, manual := range []bool{false, true} {
				for _, double := range []bool{false, true} {
					t.Run(fmt.Sprintf("compact=%t/offset=%d/manual=%t/double=%t", compact, offset, manual, double), func(t *testing.T) {
						a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
						a.st.SidebarSize, a.st.SidebarManual = 42, manual
						a.st.SidebarVisible = !compact
						path := filepath.Join(t.TempDir(), "preferences.json")
						a.preferenceSave = func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) }
						a.saveViewPreferences()
						check := func(visible bool, width int, manual bool) {
							t.Helper()
							p, err := endpoint.LoadViewPreferences(path)
							if err != nil || p == nil {
								t.Fatalf("read saved preferences: %v, %v", p, err)
							}
							if a.st.SidebarVisible != visible || a.st.SidebarHidden || a.st.SidebarSize != width || a.st.ExpandedSidebarWidth() != width || a.st.SidebarManual != manual {
								t.Fatalf("state: visible=%t hidden=%t width=%d expanded=%d manual=%t; want %t/false/%d/%d/%t", a.st.SidebarVisible, a.st.SidebarHidden, a.st.SidebarSize, a.st.ExpandedSidebarWidth(), a.st.SidebarManual, visible, width, width, manual)
							}
							if p.SidebarWidth != width || p.SidebarManual != manual || p.SidebarCollapsed == visible || p.SidebarHidden {
								t.Fatalf("saved preferences: %+v; want width=%d manual=%t collapsed=%t hidden=false", p, width, manual, !visible)
							}
						}
						m := uv.Mouse{X: a.st.SidebarArea().W + offset, Y: 5, Button: uv.MouseLeft}
						if !a.sidebarPress(m) || a.sidebarGesture == nil || !a.sidebarGesture.divider {
							t.Fatal("left divider column not grabbed")
						}
						now := time.Now()
						click := func(at time.Time) {
							a.sidebarGestureMouseAt(input.Event{Kind: input.KindMouseMotion, Mouse: m}, at)
							check(!compact, 42, manual)
							a.sidebarGestureMouseAt(input.Event{Kind: input.KindMouseRelease, Mouse: m}, at)
						}
						click(now)
						a.flushSidebarDividerClicks(now.Add(sidebarDividerDoubleClick))
						check(!compact, 42, manual)
						if double {
							a.beginSidebarDivider(m, false, now.Add(sidebarDividerDoubleClick))
							click(now.Add(sidebarDividerDoubleClick))
						}
						a.flushSidebarDividerClicks(now.Add(2*sidebarDividerDoubleClick + time.Nanosecond))
						if double {
							_, _, normal := a.st.SidebarBounds()
							check(true, normal, false)
						} else {
							check(compact, 42, manual)
						}
					})
				}
			}
		}
	}
}

func TestLeftSidebarDividerBothColumnsStillDrag(t *testing.T) {
	for _, offset := range []int{-1, 0} {
		a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		x := a.st.SidebarArea().W + offset
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: 5, Button: uv.MouseLeft}})
		if a.sidebarGesture == nil || !a.sidebarGesture.divider {
			t.Fatal("divider edge not grabbed", offset)
		}
		a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: 33, Y: 5, Button: uv.MouseLeft}})
		a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: 33, Y: 5, Button: uv.MouseLeft}})
		if a.st.SidebarArea().W != 34 || !a.st.SidebarManual {
			t.Fatal("divider drag not applied", offset)
		}
	}
}

func TestSidebarDividerTimedSingleDoubleDragAndPreferences(t *testing.T) {
	for _, right := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			for _, gesture := range []string{"single", "double", "drag"} {
				if right && gesture == "drag" {
					continue
				}
				a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
				width := 42
				a.sidebarConfig = config.Config{}
				a.sidebarConfig.UI.RightSidebarWidth = &width
				a.st.RightSidebarSize = width
				if compact {
					if right {
						a.st.RightSidebarMode = "compact"
					} else {
						a.st.SidebarVisible = false
					}
				}
				var saved []endpoint.ViewPreferences
				a.preferenceSave = func(p endpoint.ViewPreferences) error { saved = append(saved, p); return nil }
				now := time.Unix(100, 0)
				x := a.st.SidebarArea().W - 1
				if right {
					x = a.st.RightSidebarArea().X
				}
				click := func(at time.Time) {
					a.beginSidebarDivider(uv.Mouse{X: x, Y: 5}, right, at)
					g := a.sidebarGesture
					a.sidebarGesture = nil
					a.finishSidebarDivider(g, at)
				}
				click(now)
				a.flushSidebarDividerClicks(now.Add(sidebarDividerDoubleClick))
				if len(saved) != 0 {
					t.Fatal("single committed before double window ended")
				}
				switch gesture {
				case "single":
					a.flushSidebarDividerClicks(now.Add(sidebarDividerDoubleClick + time.Nanosecond))
				case "double":
					click(now.Add(sidebarDividerDoubleClick))
				case "drag":
					a.beginSidebarDivider(uv.Mouse{X: x, Y: 5}, right, now.Add(time.Millisecond))
					to := 33
					a.sidebarGestureMouseAt(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: to, Y: 5, Button: uv.MouseLeft}}, now.Add(2*time.Millisecond))
					a.sidebarGestureMouseAt(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: to, Y: 5, Button: uv.MouseLeft}}, now.Add(3*time.Millisecond))
				}
				a.flushSidebarDividerClicks(now.Add(time.Second))
				if len(saved) != 1 {
					t.Fatal("gesture saved incorrectly", right, compact, gesture, len(saved))
				}
				if gesture == "single" {
					if right {
						if (a.st.RightSidebarMode == "compact") == compact {
							t.Fatal("single did not toggle right")
						}
					} else if a.st.SidebarVisible != compact {
						t.Fatal("single did not toggle left")
					}
				}
				if gesture == "double" {
					if right {
						if a.st.RightSidebarMode != "" || a.st.RightSidebarArea().W != 42 {
							t.Fatal("right double did not restore config")
						}
					} else if !a.st.SidebarVisible || a.st.SidebarArea().W != 26 || a.st.SidebarManual {
						t.Fatal("left double did not restore normal")
					}
				}
				if gesture == "drag" && a.st.SidebarArea().W != 34 {
					t.Fatal("left drag")
				}

				state := model.New(t.TempDir(), 160, 30)
				restored := NewApp(state, runtime.NewSupervisor(), &bytes.Buffer{}, "")
				restored.applyViewPreferences(&saved[0])
				if state.RightSidebarMode != a.st.RightSidebarMode || state.SidebarVisible != a.st.SidebarVisible || state.SidebarSize != a.st.ExpandedSidebarWidth() {
					t.Fatal("saved preference does not reproduce state")
				}
			}
		}
	}
}
