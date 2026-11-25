package render

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestRunawayDialogDrawsTargetBeforeArmingActions(t *testing.T) {
	t.Parallel()
	state := model.New(t.TempDir(), 100, 30)
	dialog := &Dialog{
		Title:          "暴走検知",
		RequiredLines:  []string{"remote-host", "s12 · p34 · PID 12345"},
		Lines:          []string{"optional details"},
		Actions:        []string{"1 安全停止 (SIGTERM)", "2 強制終了 (SIGKILL)"},
		CenterTerminal: true,
		MaxCols:        32,
	}
	draw := func(cols, rows int) (string, *Output) {
		dialog.MaxCols, dialog.MaxRows = cols, rows
		var cfg config.Config
		cfg.UI.Toast.Clipboard.Position = "top-left"
		out := ComposeClient(Input{State: state, SidebarConfig: cfg, Dialog: dialog, Copied: "old clipboard toast"}, nil, "", false)
		var text strings.Builder
		for y := range rows {
			for x := range cols {
				text.WriteString(out.Frame.Cell(x, y).Content)
			}
			text.WriteByte('\n')
		}
		return text.String(), out
	}
	short, out := draw(32, 6)
	if !strings.Contains(short, "remote-host") || !strings.Contains(short, "PID 12345") ||
		!strings.Contains(short, "Esc") || len(out.DialogActions) != 0 || strings.Contains(short, "SIGKILL") {
		t.Fatalf("short dialog: bounds %+v\n%s", out.DialogActions, short)
	}
	narrow, out := draw(19, 12)
	if strings.Contains(narrow, "PID 12345") || len(out.DialogActions) != 0 {
		t.Fatalf("clipped target enabled actions: bounds %+v\n%s", out.DialogActions, narrow)
	}
	wide, out := draw(45, 12)
	if !strings.Contains(wide, "remote-host") || !strings.Contains(wide, "PID 12345") ||
		!strings.Contains(wide, "SIGTERM") || !strings.Contains(wide, "SIGKILL") ||
		len(out.DialogActions) != 2 || out.DialogActions[0].W == 0 || out.DialogActions[1].W == 0 {
		t.Fatalf("wide dialog: bounds %+v\n%s", out.DialogActions, wide)
	}
}
