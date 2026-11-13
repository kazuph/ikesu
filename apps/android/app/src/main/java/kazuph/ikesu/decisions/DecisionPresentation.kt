package kazuph.ikesu.decisions

fun decisionHeading(title: String, body: String, kind: String): String =
    title.trim().ifEmpty { body.lineSequence().firstOrNull()?.trim().orEmpty() }.ifEmpty {
        when (kind) {
            "command_guard" -> "コマンドの承認"
            "tool_permission" -> "ツールの許可"
            "agent_prompt" -> "エージェントの質問"
            "ask" -> "確認"
            else -> "判断待ち"
        }
    }

fun optionSupplement(description: String, preview: String, previewUnavailable: Boolean): String =
    listOf(description, preview, if (previewUnavailable) "画面で確認" else "")
        .filter { it.isNotEmpty() }.joinToString("\n")

fun deadlineAndCount(deadline: String, remaining: Int): String =
    "${deadline.ifEmpty { "期限なし" }}　｜　ほか ${remaining} 件"

fun permissionDetails(explanation: String, command: String, warning: String, note: String = ""): List<Pair<String, String>> =
    listOf("理由" to explanation, "注記" to note, "コマンド" to command, "警告" to warning).filter { it.second.isNotEmpty() }

fun isFlaggedVerdict(verdict: String): Boolean = verdict == "ask" || verdict == "block"

fun unitJoiner(op: String): String = if (op == "\n") "⏎" else op

/** Red lines under a flagged command row: where it hit (target), then why and which rule. */
fun unitNotes(target: String, reason: String, rule: String): List<String> = buildList {
    if (target.isNotEmpty()) add("中で: $target")
    if (reason.isNotEmpty() || rule.isNotEmpty()) {
        add("理由: " + reason + (if (reason.isNotEmpty() && rule.isNotEmpty()) " ／ " else "") + (if (rule.isEmpty()) "" else "規則 $rule"))
    }
}

fun unitAllowNote(verdict: String): String = if (verdict == "unjudged") "未判定" else "確認なし"

/** Option heading: multi-select rows carry a checkbox, single-select rows a check on the chosen one. */
fun optionHeading(index: Int, label: String, multiSelect: Boolean, checked: Boolean): String =
    if (multiSelect) "${if (checked) "☑" else "☐"} ${index + 1}. $label" else "${if (checked) "✓ " else ""}${index + 1}. $label"

/** Shown above the options of a multi-select decision, so it is not mistaken for a single choice. */
fun multiSelectGuide(selected: Int): String =
    "複数選べます。選んだら「回答する」で送ります" + if (selected > 0) "（${selected}つ選択中）" else ""

/** The answer button names how many choices a multi-select answer carries. */
fun answerLabel(multiSelect: Boolean, selected: Int): String =
    if (multiSelect && selected > 0) "回答する（${selected}つ）" else "回答する"

/** 通知を出す判断待ち（notify=true）の id。通知が許可されていなければ空（届いたとは言えない）。 */
fun notifiedIds(items: List<Pair<String, Boolean>>, notificationsEnabled: Boolean): List<String> =
    if (!notificationsEnabled) emptyList() else items.filter { it.second && it.first.isNotEmpty() }.map { it.first }.distinct()
