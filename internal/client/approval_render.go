package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/render"
)

func (m *machineClient) approvalCanOpen(e *managedEndpoint) bool {
	if e == nil || e.snapshot == nil || e.snapshot.State == nil || m.blurred || m.pending != nil {
		return false
	}
	view := *e.snapshot
	if m.forms != nil {
		m.forms.Overlay(&view)
	}
	return view.Dialog == nil && view.Menu == nil && view.Help == nil && view.Settings == nil && view.Navigator == nil && view.Copy == nil && (view.ModeHint == "" || view.Prefix)
}

func (s *approvalState) overlay(in *render.Input) {
	in.ApprovalCounts = make(map[int]int)
	for _, d := range s.pending {
		if pane, ok := approvalPaneID(d.Origin.PaneID); ok {
			in.ApprovalCounts[pane]++
		}
	}
	d := s.current()
	if d == nil {
		return
	}
	pane, ok := approvalPaneID(d.Origin.PaneID)
	if !ok {
		return
	}
	width := 1
	for _, p := range in.State.DisplayPaneRects() {
		if p.ID == pane {
			width = max(1, min(p.Rect.W, 72)-2)
			break
		}
	}
	lines := []string{}
	add := func(value string) {
		for _, paragraph := range strings.Split(value, "\n") {
			current, cells := "", 0
			for _, r := range safeApprovalText(paragraph) {
				w := max(1, ansi.StringWidth(string(r)))
				if cells+w > width && current != "" {
					lines = append(lines, current)
					current, cells = "", 0
				}
				current += string(r)
				cells += w
			}
			lines = append(lines, current)
		}
	}
	add("出どころ: " + d.Origin.PaneID + " " + d.Origin.PaneLabel)
	if d.LateAnswer {
		if d.QuestionLabel != nil {
			add(*d.QuestionLabel)
		}
		if d.Body != nil {
			add(*d.Body)
		}
	} else if d.Kind == "tool_permission" && d.Origin.Tool != "" {
		add("道具: " + d.Origin.Tool)
		add("理由: " + d.Origin.Explanation)
		add("画面の表示どおり（折り返しを含む）")
		add("コマンド: " + d.Origin.Command)
		if d.Origin.Warning != "" {
			add("警告: " + d.Origin.Warning)
		}
		if d.ExpiresUnixMS != nil {
			add("期限: " + time.UnixMilli(*d.ExpiresUnixMS).Local().Format("15:04:05"))
		}
	} else {
		add("コマンド: " + d.Origin.Command)
		if d.Origin.TargetCommand != "" {
			add("対象: " + d.Origin.TargetCommand)
		}
		if d.Body != nil {
			add("申請文: " + *d.Body)
		}
	}
	for i, option := range d.Options {
		mark := " "
		if s.selected == option.ID {
			mark = "●"
		}
		if d.LateAnswer && d.MultiSelect {
			mark = "  [ ]"
			if s.checked[option.ID] {
				mark = "  [✓]"
			}
			if s.selected == option.ID {
				mark = ">" + mark[1:]
			}
		}
		add(fmt.Sprintf("%s %d: %s", mark, i+1, option.Label))
		if d.LateAnswer || d.Kind == "tool_permission" {
			if option.Description != nil && *option.Description != "" {
				add(*option.Description)
			}
			if option.Preview != nil && *option.Preview != "" {
				add("プレビュー: " + *option.Preview)
			} else {
				add("プレビュー: 画面で確認")
			}
		}
	}
	if d.LateAnswer && d.AllowText {
		add("自由入力: " + s.reason)
		if s.textMode {
			add("入力中（Ctrl+U: 選択に戻る）")
		}
	} else if s.selectedReject() && d.AllowText {
		add("却下理由: " + s.reason)
		add("Ctrl+U: 選択に戻る")
	}
	if s.waiting {
		add("回答を送信中")
	} else if d.LateAnswer {
		instructions := "数字: 選択"
		if d.MultiSelect {
			instructions += "  Space: チェック"
		}
		if d.AllowText {
			instructions += "  t: 自由入力"
		}
		add(instructions + "  Enter: 確定  Esc: 閉じる")
	} else {
		add("数字: 選択  Enter: 確定  Esc: 閉じる  ↑↓: スクロール")
	}
	in.Approval = &render.Approval{Pane: pane, Title: safeApprovalText(d.Title), Lines: lines, Scroll: s.scroll}
}

func approvalPaneID(value string) (int, bool) {
	if !strings.HasPrefix(value, "p") {
		return 0, false
	}
	pane, err := strconv.Atoi(strings.TrimPrefix(value, "p"))
	return pane, err == nil && pane > 0 && value == fmt.Sprintf("p%d", pane)
}

func safeApprovalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}
