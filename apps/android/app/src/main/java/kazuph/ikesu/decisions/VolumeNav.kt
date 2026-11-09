package kazuph.ikesu.decisions

/** Move on release, so a long press can consume the key without also moving. UP long = back, DOWN long = confirm. */
class VolumeNav(private val up: () -> Unit, private val down: () -> Unit, private val confirm: () -> Unit,
                private val enabled: () -> Boolean = { true }, private val back: () -> Unit = {}) {
    private var downLongFired = false
    private var upLongFired = false

    fun keyDown(key: Int, first: Boolean): Boolean {
        if (key == DOWN && first) downLongFired = false
        if (key == UP && first) upLongFired = false
        return key == UP || key == DOWN
    }

    fun keyUp(key: Int): Boolean = when (key) {
        UP -> { if (!upLongFired && enabled()) up(); true }
        DOWN -> { if (!downLongFired && enabled()) down(); true }
        else -> false
    }

    fun keyLongPress(key: Int): Boolean {
        if (key == DOWN) {
            if (downLongFired) return false
            downLongFired = true
            if (enabled()) confirm()
            return true
        }
        if (key == UP) {
            if (upLongFired) return false
            upLongFired = true
            back()
            return true
        }
        return false
    }

    companion object {
        const val UP = 24
        const val DOWN = 25
    }
}

/** One stop of the cursor on the detail screen. */
enum class Slot { OPTION, COMMENT, LATER, ANSWER }

object DetailCursor {
    fun slots(optionCount: Int, allowText: Boolean): List<Slot> =
        List(optionCount) { Slot.OPTION } + (if (allowText) listOf(Slot.COMMENT) else emptyList()) + listOf(Slot.LATER, Slot.ANSWER)
    fun answerIndex(optionCount: Int, allowText: Boolean): Int = slots(optionCount, allowText).lastIndex
}

/** Where a back action goes. */
enum class BackTarget { CLOSE_SETTINGS, TO_LIST, NOTHING }

object BackNav {
    fun target(settingsVisible: Boolean, showList: Boolean): BackTarget =
        if (settingsVisible) BackTarget.CLOSE_SETTINGS else if (!showList) BackTarget.TO_LIST else BackTarget.NOTHING
}

object TextScale {
    const val MIN = 0.6f
    const val MAX = 2.5f
    fun clamp(value: Float): Float = if (value.isNaN()) 1f else value.coerceIn(MIN, MAX)
    /** Redraw only when the change from what is drawn is big enough. */
    fun needsRedraw(applied: Float, wanted: Float): Boolean = Math.abs(wanted / applied - 1f) >= 0.04f
}

object UrlFinder {
    private val pattern = Regex("https?://[A-Za-z0-9][A-Za-z0-9\\-._~:/?#\\[\\]@!$&'()*+,;=%]*")
    fun find(text: String): List<IntRange> = pattern.findAll(text).mapNotNull { m ->
        var end = m.range.last + 1
        while (end > m.range.first) {
            val c = text[end - 1]
            val url = text.substring(m.range.first, end)
            val strip = when (c) {
                '.', ',', '\'', '"', '!', '?', ':', ';' -> true
                ')' -> url.count { it == ')' } > url.count { it == '(' }
                ']' -> url.count { it == ']' } > url.count { it == '[' }
                else -> false
            }
            if (!strip) break
            end--
        }
        val url = text.substring(m.range.first, end)
        if (Regex("^https?://[A-Za-z0-9]").containsMatchIn(url)) m.range.first until end else null
    }.toList()
}
