package server

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

// Delivery is held only in memory. A crash or forced termination loses it.
func (s *Server) setLateDelivery(id, status, reason string) {
	late := s.screenPrompts.late[id]
	if late == nil || late.delivery == nil || late.delivery.Status != "pending" {
		return
	}
	late.delivery.Status, late.delivery.Reason = status, reason

}

func (s *Server) lateDeliveryHasOtherDecision(pid int) bool {
	store, err := s.db()
	if err != nil {
		return true
	}
	pending, err := store.Decisions("pending")
	if err != nil {
		return true
	}
	for _, d := range pending {
		if d.Origin == nil || d.Origin.PaneID != fmtPaneID(pid) || d.Origin.Machine != "" && d.Origin.Machine != "local" {
			continue
		}
		if s.screenPrompts.late[d.DecisionID] == nil {
			return true
		}
	}
	return false
}

func (s *Server) lateDeliveryInFlight(pid int, currentID string) bool {
	for id, other := range s.screenPrompts.late {
		if id != currentID && other.pid == pid && other.sent && other.delivery != nil && other.delivery.Status == "pending" {
			return true
		}
	}
	return false
}

func (s *Server) scanLateCodexDeliveries() {
	for id, late := range s.screenPrompts.late {
		if late.delivery == nil || late.delivery.Status != "pending" || late.checking {
			continue
		}
		if !safeLateAnswer(late.delivery.Text) {
			s.setLateDelivery(id, "failed", "回答に送信できない制御文字が含まれています")
			continue
		}
		pane := s.sup.Get(late.pid)
		if (pane == nil || pane.Snapshot().Dead) && !late.sent {
			s.setLateDelivery(id, "failed", "ペインが閉じました")
			continue
		}
		late.checking = true
		binding := *late.w.codex
		sent, offset, previous := late.sent, late.offset, late.fileInfo
		text := late.delivery.Text
		go func(id string) {
			var fileInfo os.FileInfo
			var received bool
			var failure error
			if sent {
				fileInfo, failure = os.Stat(binding.path)
				if failure == nil && (previous == nil || !os.SameFile(previous, fileInfo)) {
					failure = fmt.Errorf("会話記録が置き換わりました")
				}
				if failure == nil {
					received, failure = session.HasCodexUserInput(binding.path, offset, text)
				}
			}
			if failure != nil || received {
				// A recorded receipt is authoritative even if the pane has since exited.
			} else if pane == nil || pane.Snapshot().Dead {
				failure = fmt.Errorf("ペインが閉じました")
			} else if foregroundAgent(pane) != "codex" {
				failure = fmt.Errorf("Codex のセッションを確認できません")
			} else {
				path, currentID, err := session.OpenCodexRollout(pane.ForegroundJob())
				if err != nil || path != binding.path || currentID != binding.question.SessionID {
					failure = fmt.Errorf("元の Codex セッションを確認できません")
				} else {
					fileInfo, failure = os.Stat(path)
					if failure == nil && previous != nil && !os.SameFile(previous, fileInfo) {
						failure = fmt.Errorf("会話記録が置き換わりました")
					}
				}
			}
			s.app.Do(func() {
				current := s.screenPrompts.late[id]
				if current != late || current.delivery == nil || current.delivery.Status != "pending" {
					return
				}
				current.checking = false
				if failure != nil {
					s.setLateDelivery(id, "failed", failure.Error())
					return
				}
				if received {
					s.setLateDelivery(id, "delivered", "")
					return
				}
				if current.sent {
					return
				}
				if s.sup.Get(current.pid) != pane || pane.Snapshot().Dead {
					s.setLateDelivery(id, "failed", "ペインが閉じました")
					return
				}
				status := s.app.PaneAgentStatusKey(current.pid)
				if status != string(api.AgentIdle) && status != string(api.AgentDone) {
					return
				}
				visible, _ := pane.ReadLines("visible", true)
				// The launch-readiness deadline can expire while a real question is open.
				// A live Codex process, idle status, and the current screen decide delivery.
				if s.lateDeliveryInFlight(current.pid, id) || s.lateDeliveryHasOtherDecision(current.pid) || s.screenUserActive(current.pid) || s.readScreenPrompt(current.pid) != nil || screenprompt.HasOptionRows(visible) {
					return
				}
				// Repeat process/session and terminal-state checks immediately before the single write.
				path, currentID, err := session.OpenCodexRollout(pane.ForegroundJob())
				if err != nil || foregroundAgent(pane) != "codex" || path != binding.path || currentID != binding.question.SessionID {
					s.setLateDelivery(id, "failed", "元の Codex セッションを確認できません")
					return
				}
				status = s.app.PaneAgentStatusKey(current.pid)
				visible, _ = pane.ReadLines("visible", true)
				if (status != string(api.AgentIdle) && status != string(api.AgentDone)) || s.lateDeliveryInFlight(current.pid, id) || s.lateDeliveryHasOtherDecision(current.pid) || s.readScreenPrompt(current.pid) != nil || screenprompt.HasOptionRows(visible) || s.screenUserActive(current.pid) {
					return
				}
				fresh, err := os.Stat(path)
				if err != nil || !os.SameFile(fileInfo, fresh) {
					s.setLateDelivery(id, "failed", "会話記録を確認できません")
					return
				}
				if _, err = session.HasCodexUserInput(path, fresh.Size(), text); err != nil {
					s.setLateDelivery(id, "failed", "会話記録を確認できません")
					return
				}
				payload := text
				if pane.BracketedPasteMode() {
					payload = "\x1b[200~" + payload + "\x1b[201~"
				} else if strings.ContainsAny(payload, "\r\n\x1b") {
					s.setLateDelivery(id, "failed", "複数行の回答を安全に送れません")
					return
				}
				current.sent, current.offset, current.fileInfo = true, fresh.Size(), fresh
				sequence := pane.ExternalInputSequence()
				_, err = pane.WriteScreenPrompt([]byte(payload+"\r"), &sequence, time.Time{})
				if err != nil {
					s.setLateDelivery(id, "failed", "端末への書き込みに失敗しました: "+err.Error())
				}
			})
		}(id)
	}
}

func safeLateAnswer(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}
