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
