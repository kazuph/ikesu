package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestEndpointMobileToastUsesObservedBottomBand(t *testing.T) {
	state := testState(60, 45)
	state.MobileWidthThreshold = 64
	for _, tc := range []struct{ kind, title, want string }{
		{"info", "TWO_DISPLAY", " ● update ready · BODY"},
		{"finished", "2 background", " ● 2 background · BODY"},
	} {
		toast := &Toast{Kind: tc.kind, Title: tc.title, Body: "BODY"}
		out := Compose(Input{State: state, ServerUI: true, Toast: toast})
		var line strings.Builder
		for x := 0; x < 60; x++ {
			line.WriteString(out.Frame.Cell(x, 44).Content)
		}
		if line.String() != tc.want+strings.Repeat(" ", 60-len([]rune(tc.want))) || out.ToastRect != (layout.Rect{Y: 44, W: 60, H: 1}) {
			t.Fatal("mobile notification differs from observed band", line.String(), out.ToastRect)
		}
		toast.Position = "top-right"
		explicit := Compose(Input{State: state, ServerUI: true, Toast: toast})
		if !reflect.DeepEqual(explicit.Frame.Cells, out.Frame.Cells) {
			t.Fatal("position changed mobile notification placement")
		}
	}
}
