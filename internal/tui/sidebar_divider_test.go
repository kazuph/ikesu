package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
	"testing"
	"time"
)

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
				if gesture == "drag" {
					if right {
						if a.st.RightSidebarArea().W != 38 {
							t.Fatal("right drag")
						}
					} else if a.st.SidebarArea().W != 34 {
						t.Fatal("left drag")
					}
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
