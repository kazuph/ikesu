package kazuph.ikesu.decisions

/** Move on release, so a long press can consume the down key without also moving. */
class VolumeNav(private val up: () -> Unit, private val down: () -> Unit, private val confirm: () -> Unit) {
    private var downLongFired = false

    fun keyDown(key: Int, first: Boolean): Boolean {
        if (key == DOWN && first) downLongFired = false
        return key == UP || key == DOWN
    }

    fun keyUp(key: Int): Boolean = when (key) {
        UP -> { up(); true }
        DOWN -> { if (!downLongFired) down(); true }
        else -> false
    }

    fun keyLongPress(key: Int): Boolean {
        if (key != DOWN || downLongFired) return false
        downLongFired = true
        confirm()
        return true
    }

    companion object {
        const val UP = 24
        const val DOWN = 25
    }
}
