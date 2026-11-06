package kazuph.ikesu.decisions

import org.junit.Assert.assertEquals
import org.junit.Test

class DecisionPresentationTest {
    @Test fun permissionDetailsKeepFullValuesAndOrder() {
        assertEquals(listOf("理由" to "説明", "コマンド" to "printf 'long command'", "警告" to "危険"), permissionDetails("説明", "printf 'long command'", "危険"))
        assertEquals(listOf("コマンド" to "echo x"), permissionDetails("", "echo x", ""))
    }
    @Test fun headingUsesOriginalTitleThenBodyThenKind() {
        assertEquals("題名", decisionHeading("題名", "本文\n続き", "agent_prompt"))
        assertEquals("本文", decisionHeading("", "本文\n続き", "ask"))
        assertEquals("コマンドの承認", decisionHeading("", "", "command_guard"))
        assertEquals("判断待ち", decisionHeading(" ", "", "other"))
    }
    @Test fun onlyUnavailablePreviewsShowTheFallback() {
        assertEquals("", optionSupplement("", "", false))
        assertEquals("説明", optionSupplement("説明", "", false))
        assertEquals("説明\nプレビュー", optionSupplement("説明", "プレビュー", false))
        assertEquals("説明\n画面で確認", optionSupplement("説明", "", true))
    }
    @Test fun deadlineAndCountAlwaysAppearTogether() {
        assertEquals("期限なし　｜　ほか 0 件", deadlineAndCount("", 0))
        assertEquals("期限 12:30（残り約5分）　｜　ほか 2 件", deadlineAndCount("期限 12:30（残り約5分）", 2))
    }
}
