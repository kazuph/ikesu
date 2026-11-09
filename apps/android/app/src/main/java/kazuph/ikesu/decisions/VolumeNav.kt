package kazuph.ikesu.decisions

/** Move on release, so a long press can consume the key without also moving. UP long = back, DOWN long = confirm. */
class VolumeNav(private val up: () -> Unit, private val down: () -> Unit, private val confirm: () -> Unit,
                private val enabled: () -> Boolean = { true }, private val back: () -> Unit = {}) {
    private var downLongFired = false
    private var upLongFired = false
    private var ignoreNextUp = false

    /** Drop the next UP release once (the press that closed the full-screen image). A fresh press clears this. */
    fun ignoreNextUpRelease() { ignoreNextUp = true }

    fun keyDown(key: Int, first: Boolean): Boolean {
        if (key == DOWN && first) downLongFired = false
        if (key == UP && first) { upLongFired = false; ignoreNextUp = false }
        return key == UP || key == DOWN
    }

    fun keyUp(key: Int): Boolean = when (key) {
        UP -> { if (ignoreNextUp) ignoreNextUp = false else if (!upLongFired && enabled()) up(); true }
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

/** Cursor position kept as a kind plus the option id, so it survives a changed list of options. */
data class CursorRef(val slot: Slot = Slot.OPTION, val optionId: String? = null)
data class CursorResolved(val index: Int, val ref: CursorRef, val navigated: Boolean)

object DetailCursor {
    fun resolve(ref: CursorRef, optionIds: List<String>, allowText: Boolean, navigated: Boolean): CursorResolved {
        val n = optionIds.size
        val slots = slots(n, allowText)
        val index = when (ref.slot) {
            Slot.OPTION -> optionIds.indexOf(ref.optionId).takeIf { it >= 0 }
            Slot.COMMENT -> if (allowText) n else null
            Slot.LATER -> slots.indexOf(Slot.LATER)
            Slot.ANSWER -> slots.lastIndex
        }
        if (index != null) return CursorResolved(index, ref, navigated)
        // The pointed option or the comment box is gone: back to the first option, not to a button.
        return if (n > 0) CursorResolved(0, CursorRef(Slot.OPTION, optionIds[0]), false)
        else CursorResolved(slots.lastIndex, CursorRef(Slot.ANSWER), false)
    }
    fun refAt(index: Int, optionIds: List<String>, allowText: Boolean): CursorRef {
        val slots = slots(optionIds.size, allowText)
        return when (val slot = slots.getOrElse(index) { Slot.ANSWER }) {
            Slot.OPTION -> CursorRef(slot, optionIds[index])
            else -> CursorRef(slot)
        }
    }

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
    /** The debug-only intent extra: null means ignore it. */
    fun fromIntent(debuggable: Boolean, present: Boolean, value: Float): Float? = if (debuggable && present) clamp(value) else null
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
                '.', ',', '\'', '"', '!', '?', ':', ';', '*' -> true
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
