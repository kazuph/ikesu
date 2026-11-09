package kazuph.ikesu.decisions

import org.junit.Assert.assertEquals
import org.junit.Test

class VolumeNavTest {
    @Test fun settingsPreventHiddenDecisionActions() {
        val events = mutableListOf<String>()
        var settingsVisible = true
        val nav = VolumeNav({ events.add("up") }, { events.add("down") }, { events.add("confirm") }, { !settingsVisible })
        repeat(2) {
            nav.keyDown(VolumeNav.DOWN, true)
            nav.keyLongPress(VolumeNav.DOWN)
            nav.keyUp(VolumeNav.DOWN)
        }
        nav.keyDown(VolumeNav.UP, true)
        nav.keyUp(VolumeNav.UP)
        assertEquals(emptyList<String>(), events)
        settingsVisible = false
        nav.keyDown(VolumeNav.DOWN, true)
        nav.keyLongPress(VolumeNav.DOWN)
        nav.keyUp(VolumeNav.DOWN)
        assertEquals(listOf("confirm"), events)
    }
    @Test fun longPressDoesNotAlsoMoveOnRelease() {
        val events = mutableListOf<String>()
        val nav = VolumeNav({ events.add("up") }, { events.add("down") }, { events.add("confirm") })
        nav.keyDown(VolumeNav.DOWN, true)
        nav.keyLongPress(VolumeNav.DOWN)
        nav.keyDown(VolumeNav.DOWN, false)
        nav.keyUp(VolumeNav.DOWN)
        assertEquals(listOf("confirm"), events)
        nav.keyDown(VolumeNav.DOWN, true)
        nav.keyUp(VolumeNav.DOWN)
        nav.keyDown(VolumeNav.UP, true)
        nav.keyUp(VolumeNav.UP)
        assertEquals(listOf("confirm", "down", "up"), events)
    }
}

class VolumeNavBackTest {
    @Test fun upLongPressGoesBackAndDoesNotMoveOnRelease() {
        val events = mutableListOf<String>()
        var settings = true
        val nav = VolumeNav({ events.add("up") }, { events.add("down") }, { events.add("confirm") }, { !settings }, { events.add("back") })
        nav.keyDown(VolumeNav.UP, true); nav.keyLongPress(VolumeNav.UP); nav.keyDown(VolumeNav.UP, false); nav.keyLongPress(VolumeNav.UP); nav.keyUp(VolumeNav.UP)
        assertEquals(listOf("back"), events)
        settings = false
        nav.keyDown(VolumeNav.UP, true); nav.keyUp(VolumeNav.UP)
        assertEquals(listOf("back", "up"), events)
    }
    @Test fun backTargets() {
        assertEquals(BackTarget.CLOSE_SETTINGS, BackNav.target(true, false))
        assertEquals(BackTarget.CLOSE_SETTINGS, BackNav.target(true, true))
        assertEquals(BackTarget.TO_LIST, BackNav.target(false, false))
        assertEquals(BackTarget.NOTHING, BackNav.target(false, true))
    }
    @Test fun slotsOrder() {
        assertEquals(listOf(Slot.OPTION, Slot.OPTION, Slot.COMMENT, Slot.LATER, Slot.ANSWER), DetailCursor.slots(2, true))
        assertEquals(listOf(Slot.OPTION, Slot.LATER, Slot.ANSWER), DetailCursor.slots(1, false))
        assertEquals(1, DetailCursor.answerIndex(0, false))
    }
    @Test fun textScaleClampAndRedraw() {
        assertEquals(0.6f, TextScale.clamp(0.1f), 0f)
        assertEquals(2.5f, TextScale.clamp(9f), 0f)
        assertEquals(1.3f, TextScale.clamp(1.3f), 0f)
        assertEquals(false, TextScale.needsRedraw(1f, 1.02f))
        assertEquals(true, TextScale.needsRedraw(1f, 1.1f))
        assertEquals(true, TextScale.needsRedraw(1f, 0.9f))
    }
}

class UrlFinderTest {
    private fun urls(s: String) = UrlFinder.find(s).map { s.substring(it) }
    @Test fun fullWidthParens() = assertEquals(listOf("https://x.com/a/pull/113"), urls("（https://x.com/a/pull/113）"))
    @Test fun stopsAtFullWidthPeriod() = assertEquals(listOf("https://x.com/a"), urls("https://x.com/a。次"))
    @Test fun stopsAtFullWidthParenThenKana() = assertEquals(listOf("https://x.com/a"), urls("https://x.com/a）みたい"))
    @Test fun balancedParens() = assertEquals(listOf("https://en.wikipedia.org/wiki/Foo_(bar)"), urls("(https://en.wikipedia.org/wiki/Foo_(bar))"))
    @Test fun trailingDot() = assertEquals(listOf("https://x.com/a"), urls("https://x.com/a."))
    @Test fun trailingMixed() = assertEquals(listOf("https://x.com/a"), urls("see \"https://x.com/a\"!?"))
    @Test fun queryAndFragment() = assertEquals(listOf("https://x.com/?q=1&b=2#h"), urls("https://x.com/?q=1&b=2#h"))
    @Test fun twoUrls() = assertEquals(listOf("http://a.example/x", "https://b.example/y"), urls("見て http://a.example/x と、https://b.example/y。"))
    @Test fun noScheme() = assertEquals(emptyList<String>(), urls("example.com と https:// だけ"))
}
