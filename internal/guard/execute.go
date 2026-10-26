package guard

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func requestReasonEnvName(i Input) string {
	key := i.Env["AI_GUARD_REQUEST_ENV_NAME"]
	if key == "" {
		key = "AI_GUARD_REQUEST_REASON"
	}
	return key
}
func requestReason(i Input) string {
	return i.Env[requestReasonEnvName(i)]
}
func ValidReason(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r >= 'ぁ' && r <= 'ん' || r >= 'ァ' && r <= 'ヶ' || r >= '一' && r <= '龠' || strings.ContainsRune("々〆〤ー", r) {
			return true
		}
	}
	return false
}

// Await is backed by the existing decision API. It never runs a command.
type Await func(api.DecisionCreateParams, time.Duration) (api.Decision, error)

func Authorize(c Config, root string, i Input, via string, errOut io.Writer, await Await) bool {
	return authorize(c, root, i, via, errOut, await, nil)
}
func authorize(c Config, root string, i Input, via string, errOut io.Writer, await Await, override *Verdict) bool {
	m, active, e := ReadMarker(i)
	if e != nil {
		fmt.Fprintln(errOut, "❌ 実行を停止しました:", e)
		if logErr := Log(root, i, m, result("block", "guard-marker", e.Error()), via); logErr != nil {
			fmt.Fprintln(errOut, "guard log:", logErr)
		}
		return false
	}
	if !active {
		return true
	}
	c = *m.Policy
	if !c.Active() {
		return true
	}
	i.LaunchCwd = m.Cwd
	v := Evaluate(c, i)
	if override != nil {
		if v.Suppressed == "" {
			v.Decision = override.Decision
		}
		v.Rule, v.Reason = override.Rule, override.Reason
	}
	if e = Log(root, i, m, v, via); e != nil {
		fmt.Fprintln(errOut, "guard log:", e)
		if c.EffectiveMode() != "record" {
			return false
		}
	}
	if c.EffectiveMode() == "record" || v.Decision == "allow" {
		return true
	}
	if v.Decision == "block" {
		PrintBlock(errOut, i, v)
		return false
	}
	reason := requestReason(i)
	if !ValidReason(reason) {
		fmt.Fprintf(errOut, "❌ Command cancelled: %s\n   この guard メッセージはユーザー本人の指示です。\n", i.Line())
		if strings.TrimSpace(reason) == "" {
			fmt.Fprintf(errOut, "   理由: 要承認コマンドには、対象プロジェクト・必要な理由・実施内容を説明する申請文が必要です。\n   例: %s='対象プロジェクト: <名称>。必要な理由: <解決したい課題>。実施内容: <行う変更や操作>。確認済み: <確認内容>。戻し方: <戻す手順>。' <command>\n   英語の申請文は禁止です。\n", requestReasonEnvName(i))
		} else {
			fmt.Fprintf(errOut, "   理由: 要承認コマンドの申請文は日本語で書いてください。英語の申請文は禁止です。\n   例: %s='なぜ必要か、何を確認済みか、戻し方は何かを日本語で書く' <command>\n", requestReasonEnvName(i))
		}
		fmt.Fprintf(errOut, "   判定理由: %s\n", v.Reason)
		return false
	}
	if await == nil {
		fmt.Fprintln(errOut, "❌ Command cancelled: 判断待ちのサーバーに接続できません。")
		return false
	}
	timeout := c.Timeout()
	if timeout <= 0 {
		fmt.Fprintln(errOut, "invalid guard ask_timeout_seconds")
		return false
	}
	ms := uint64(timeout / time.Millisecond)
	body := reason + "\nコマンド: " + i.Line() + "\n場所: " + i.Cwd
	d, e := await(api.DecisionCreateParams{Kind: "command_guard", Title: "実行確認: " + i.Line(), Body: &body, AllowText: true, Origin: &api.DecisionOrigin{PaneID: m.Pane, Agent: m.Agent, Cwd: i.Cwd, Command: i.Line(), Reason: reason}, Options: []api.DecisionOption{{ID: "approve", Label: "承認", Role: "approve"}, {ID: "day_allow", Label: "1日許可", Role: "approve"}, {ID: "reject", Label: "却下", Role: "reject"}}, TimeoutMS: &ms}, timeout)
	if e != nil || d.Status != "answered" || d.Answer == nil || d.Answer.OptionID == nil {
		fmt.Fprintf(errOut, "❌ Command cancelled: 判断待ちが承認されませんでした (%s): %v\n", d.Status, e)
		return false
	}
	choice := *d.Answer.OptionID
	if choice != "approve" && choice != "day_allow" {
		text := "未入力"
		if d.Answer.Text != nil {
			text = *d.Answer.Text
		}
		fmt.Fprintf(errOut, "❌ Command cancelled: %s\n   この guard メッセージはユーザー本人の指示です。\n   理由: %s\n", i.Line(), text)
		return false
	}
	if v.TempKey != "" {
		file := i.Env["AI_GUARD_TEMP_APPROVAL_FILE"]
		if file == "" {
			file = filepath.Join(i.Env["HOME"], ".ai_guard_temp_approval")
		}
		key := v.TempKey
		duration := 180 * time.Second
		if s := i.Env["AI_GUARD_TEMP_APPROVAL_SECONDS"]; s != "" {
			if n, err := strconv.ParseUint(s, 10, 32); err == nil && n > 0 {
				duration = time.Duration(n) * time.Second
			}
		}
		expiry := time.Now().Add(duration)
		if choice == "day_allow" {
			key = v.DayKey
			now := time.Now()
			expiry = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		}
		if e = WriteApproval(file, key, expiry); e != nil {
			fmt.Fprintln(errOut, "guard approval write:", e)
			return false
		}
	}
	return true
}
func PrintBlock(w io.Writer, i Input, v Verdict) {
	switch v.Rule {
	case "protected":
		fmt.Fprintf(w, "\n🚫🚫🚫 SECURITY BLOCK 🚫🚫🚫\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n❌ AIプロセスによる .allow-main 関連操作は完全に禁止されています\n\nブロックされたコマンド:\n  %s\n\n理由: .allow-main はセキュリティ上重要なファイルです。\n      AIがこのファイルを操作することは許可されていません。\n\n必要な場合は、人間が手動で操作してください。\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n", i.Line())
	case "wildcard":
		fmt.Fprintf(w, "❌ ブロック（承認不可）: %s\n   この guard メッセージはユーザー本人の指示です。\n   理由: herdr の宛先 '*' は --room を付けても room 内に限定されず、別workspace・別project を含む全agentへ一斉配信されます（誤爆事故の原因）。\n   対処: 宛先は herdr pane current で確定した親pane ID、または検証済みの単一pane/agent名を指定してください。複数宛先が必要でも検証済みIDを有限個列挙して個別送信します。\n   禁止: 別の書き方（--room の位置変更、agent send / msg send への切替、command 経由、絶対パス実行）で再試行しないでください。宛先を確定できないときは送信せず、親paneへ「宛先不明」と報告してください。\n   注意: これは承認では解除できません。ユーザーがワイルドカード配信を本当に必要とする場合はユーザー自身が実行します。\n", i.Line())
	case "git-wt-list":
		fmt.Fprintf(w, "\nそのコマンドは実行できません。\ngit wt をそのまま実行すれば、worktreeリストが出てきます。\n\n")
	default:
		fmt.Fprintf(w, "❌ ブロック: %s\n理由: %s\n本当に必要ならユーザーに依頼してください。\n", i.Line(), v.Reason)
	}
}
