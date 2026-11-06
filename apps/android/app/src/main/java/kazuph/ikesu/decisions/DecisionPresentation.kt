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
