package kazuph.ikesu.decisions

import org.junit.Assert.assertEquals
import org.junit.Test

class DecisionPresentationTest {
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
