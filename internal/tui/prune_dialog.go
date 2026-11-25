package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
)

type PruneAction int

const (
	PruneNoAction PruneAction = iota
	PruneStartScan
	PruneStartExec
	PruneClose
)

type pruneStage int

const (
	pruneScope pruneStage = iota
	pruneScanning
	prunePreview
	pruneConfirm
	pruneExecuting
	pruneSummary
)

type PruneEntry struct {
	Machine, Label, Server string
	Candidate              endpoint.PruneCandidate
	Selected               bool
}

// PruneDialog presents the same review and confirmation in the machine client
// and the standalone TUI. Transport and filesystem work stay with the caller.
type PruneDialog struct {
	stage       pruneStage
	cursor      int
	localOnly   bool
	activeLabel string
	labels      []string
	entries     []PruneEntry
	protected   map[string]int
	issues      []string
	outcomes    []string
	bytes       uint64
	deleted     int
	skipped     int
	failed      int
	unknown     int
}

func NewPruneDialog(active string, labels []string) *PruneDialog {
	if len(labels) == 1 {
		active = labels[0]
	}
	return &PruneDialog{activeLabel: active, labels: labels, protected: map[string]int{}}
}

func (d *PruneDialog) LocalOnly() bool { return d.localOnly }

func (d *PruneDialog) AddScan(id, label string, result endpoint.PruneScanResult) {
	for _, c := range result.Candidates {
		d.entries = append(d.entries, PruneEntry{Machine: id, Label: label, Server: result.ServerGeneration, Candidate: c, Selected: true})
	}
	for reason, count := range result.Protected {
		d.protected[reason] += count
	}
}

func (d *PruneDialog) ScanIssue(label, reason string) {
	d.issues = append(d.issues, fmt.Sprintf("%s · %s", label, reason))
}

func (d *PruneDialog) Ready() {
	sort.SliceStable(d.entries, func(i, j int) bool {
		a, b := d.entries[i], d.entries[j]
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		if a.Machine != b.Machine {
			return a.Machine < b.Machine
		}
		return a.Candidate.Workspace < b.Candidate.Workspace
	})
	sort.Strings(d.issues)
	d.stage, d.cursor = prunePreview, 0
}

func (d *PruneDialog) Selection() []PruneEntry {
	var selected []PruneEntry
	for _, e := range d.entries {
		if e.Selected {
			selected = append(selected, e)
		}
	}
	return selected
}

func (d *PruneDialog) AddOutcome(label, space string, outcome endpoint.PruneOutcome) {
	switch outcome.State {
	case endpoint.PruneDeleted:
		d.deleted++
		d.bytes += outcome.Bytes
		return
	case endpoint.PruneSkipped:
		d.skipped++
	case endpoint.PruneFailed:
		d.failed++
	default:
		d.unknown++
	}
	d.outcomes = append(d.outcomes, fmt.Sprintf("%s · %s · %s", label, space, outcome.Reason))
}

func (d *PruneDialog) Complete() {
	sort.Strings(d.outcomes)
	d.stage, d.cursor = pruneSummary, 0
}

func pruneSize(bytes uint64) string {
	for _, unit := range []struct {
		size uint64
		name string
	}{{1_000_000_000, "GB"}, {1_000_000, "MB"}, {1_000, "KB"}} {
		if bytes >= unit.size {
			return fmt.Sprintf("%.2f %s", float64(bytes)/float64(unit.size), unit.name)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

func (d *PruneDialog) total(selectedOnly bool) (count int, bytes uint64) {
	for _, entry := range d.entries {
		if !selectedOnly || entry.Selected {
			count++
			bytes += entry.Candidate.Bytes
		}
	}
	return
}

func (d *PruneDialog) previewRows() int { return len(d.entries) + len(d.issues) }

// The preview reserves one line for the protection summary.
func (d *PruneDialog) previewWindow(area layout.Rect) (int, int) {
	rows := max(1, min(9, area.H-6))
	start := max(0, d.cursor-rows+1)
	return start, min(d.previewRows(), start+rows)
}

func (d *PruneDialog) Render(area layout.Rect) *render.Dialog {
	box := &render.Dialog{Title: "prune merged workspaces", CenterTerminal: true, PreserveTitleCase: true, Confirm: "close", Cancel: "cancel"}
	switch d.stage {
	case pruneScope:
		box.Detail = "マージ済みブランチの worktree・スペース・ペインを削除します。"
		box.Lines = []string{"未コミット変更・未マージ・稼働中エージェント・HQ (親鯉) は自動保護されます。"}
		box.Confirm = "scan"
		if len(d.labels) > 1 {
			all, local := "●", "○"
			if d.localOnly {
				all, local = local, all
			}
			box.Lines = append(box.Lines, all+" 接続中の全マシン ("+strings.Join(d.labels, ", ")+")", local+" このマシンのみ ("+d.activeLabel+")")
		} else {
			box.Lines = append(box.Lines, "このマシンのみ ("+d.activeLabel+")")
		}
	case pruneScanning:
		box.Detail = "scanning merged worktrees & disk usage…"
	case prunePreview:
		count, bytes := d.total(false)
		selected, selectedBytes := d.total(true)
		if count == 0 {
			box.Detail = "削除対象のマージ済み worktree はありません"
		} else {
			box.Detail = fmt.Sprintf("reclaimable: %s (%d workspaces) · selected: %s (%d)", pruneSize(bytes), count, pruneSize(selectedBytes), selected)
			if selected > 0 {
				box.Confirm = "review deletion"
			}
		}
		protected := make([]string, 0, len(d.protected))
		for reason, n := range d.protected {
			protected = append(protected, fmt.Sprintf("%s %d", reason, n))
		}
		sort.Strings(protected)
		parts := []string{}
		if len(protected) > 0 {
			total := 0
			for _, n := range d.protected {
				total += n
			}
			parts = append(parts, fmt.Sprintf("protected: %d (%s)", total, strings.Join(protected, " · ")))
		}
		if count > 0 {
			parts = append(parts, "space toggle · a all")
		}
		box.Lines = append(box.Lines, strings.Join(parts, "  |  "))
		start, end := d.previewWindow(area)
		for i := start; i < end; i++ {
			pointer := " "
			if i == d.cursor {
				pointer = ">"
			}
			if i >= len(d.entries) {
				box.Lines = append(box.Lines, pointer+" "+d.issues[i-len(d.entries)])
				continue
			}
			e := d.entries[i]
			tick := " "
			if e.Selected {
				tick = "x"
			}
			c := e.Candidate
			prefix := fmt.Sprintf("%s [%s] ", pointer, tick)
			suffix := fmt.Sprintf(" · %s · merged into %s", pruneSize(c.Bytes), pruneDisplayText(c.Revision.Base, area))
			labels := []string{pruneDisplayText(e.Label, area), pruneDisplayText(c.Name, area), pruneDisplayText(c.Revision.Branch, area)}
			line := prefix + render.BudgetLabels(labels, area.W-8-ansi.StringWidth(prefix+suffix)) + suffix
			box.Lines = append(box.Lines, line)
		}
	case pruneConfirm:
		count, bytes := d.total(true)
		box.Detail = fmt.Sprintf("Delete %d workspaces and their worktrees (about %s)?", count, pruneSize(bytes))
		box.Lines = []string{"This closes their panes and removes the checkout directories."}
		box.Confirm, box.Cancel = "delete", "back"
	case pruneExecuting:
		box.Detail = "deleting selected workspaces and worktrees…"
		box.ContinueOnly, box.Confirm = true, ""
	case pruneSummary:
		box.Detail = fmt.Sprintf("%d 件削除 · 約 %s 解放", d.deleted, pruneSize(d.bytes))
		box.Lines = []string{fmt.Sprintf("skipped %d · failed %d · unknown %d", d.skipped, d.failed, d.unknown)}
		rows := max(1, min(9, area.H-6))
		start := max(0, d.cursor-rows+1)
		box.Lines = append(box.Lines, d.outcomes[start:min(len(d.outcomes), start+rows)]...)
	}
	box.Detail = pruneDisplayText(box.Detail, area)
	box.Width = ansi.StringWidth(box.Detail) + 5
	for i, line := range box.Lines {
		box.Lines[i] = pruneDisplayText(line, area)
		box.Width = max(box.Width, ansi.StringWidth(box.Lines[i])+5)
	}
	return box
}

func pruneDisplayText(text string, area layout.Rect) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	return ansi.Truncate(text, max(1, area.W-8), "…")
}

func (d *PruneDialog) Input(ev input.Event, area layout.Rect) PruneAction {
	if d.stage == pruneExecuting {
		return PruneNoAction
	}
	var key rune
	switch {
	case ev.Kind == input.KindKey:
		key = ev.Key.Code
	case ev.Kind == input.KindMouseRelease && ev.Mouse.Button == uv.MouseLeft:
		box := d.Render(area)
		outer, cancel, confirm := render.DialogRects(box, area)
		x, y := ev.Mouse.X, ev.Mouse.Y
		switch {
		case cancel.Contains(x, y):
			key = uv.KeyEscape
		case confirm.Contains(x, y):
			key = uv.KeyEnter
		case outer.Contains(x, y):
			row := y - outer.Y - 3
			if d.stage == pruneScope && len(d.labels) > 1 && (row == 1 || row == 2) {
				d.localOnly = row == 2
			} else if d.stage == prunePreview {
				start, end := d.previewWindow(area)
				index := start + row - 1
				if row >= 1 && index < end {
					d.cursor = index
					key = ' '
				}
			}
		}
	}
	if key == uv.KeyEscape {
		if d.stage == pruneConfirm {
			d.stage = prunePreview
			return PruneNoAction
		}
		return PruneClose
	}
	switch d.stage {
	case pruneScope:
		switch key {
		case uv.KeyUp, uv.KeyDown, uv.KeyTab:
			if len(d.labels) > 1 {
				d.localOnly = !d.localOnly
			}
		case uv.KeyEnter:
			d.stage = pruneScanning
			return PruneStartScan
		}
	case pruneScanning, pruneSummary:
		switch key {
		case uv.KeyEnter:
			return PruneClose
		case uv.KeyUp:
			d.cursor = max(0, d.cursor-1)
		case uv.KeyDown:
			d.cursor = min(d.cursor+1, max(0, len(d.outcomes)-1))
		}
	case prunePreview:
		switch key {
		case uv.KeyUp:
			d.cursor = max(0, d.cursor-1)
		case uv.KeyDown, uv.KeyTab:
			d.cursor = min(d.cursor+1, max(0, d.previewRows()-1))
		case ' ':
			if d.cursor < len(d.entries) {
				d.entries[d.cursor].Selected = !d.entries[d.cursor].Selected
			}
		case 'a':
			selected, _ := d.total(true)
			for i := range d.entries {
				d.entries[i].Selected = selected < len(d.entries)
			}
		case uv.KeyEnter:
			if len(d.Selection()) > 0 {
				d.stage = pruneConfirm
			} else {
				return PruneClose
			}
		}
	case pruneConfirm:
		if key == uv.KeyEnter {
			d.stage = pruneExecuting
			return PruneStartExec
		}
	}
	return PruneNoAction
}
