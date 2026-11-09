package kazuph.ikesu.decisions

import android.app.Dialog
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Matrix
import android.graphics.Paint
import android.view.GestureDetector
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import android.view.ViewGroup

/** Pure calculations for the full-screen image: scale range, pan limits, double-tap toggle. */
object ZoomMath {
    const val MIN = 1f
    const val MAX = 6f
    const val DOUBLE_TAP = 2.5f
    fun clampScale(value: Float): Float = if (value.isNaN()) MIN else value.coerceIn(MIN, MAX)
    /** Largest pan distance from the centered position, per axis. fit = displayed size at 1x. */
    fun maxOffset(fit: Float, scale: Float, view: Float): Float = Math.max(0f, (fit * scale - view) / 2f)
    fun clampOffset(offset: Float, fit: Float, scale: Float, view: Float): Float {
        val limit = maxOffset(fit, scale, view)
        return offset.coerceIn(-limit, limit)
    }
    fun toggle(scale: Float): Float = if (scale > MIN + 0.01f) MIN else DOUBLE_TAP
    /** Offset after zooming from [oldScale] to [newScale] around [focus] (measured from the view center). */
    fun zoomAbout(offset: Float, focus: Float, oldScale: Float, newScale: Float): Float = focus - (focus - offset) * (newScale / oldScale)
}

/** Whether the viewer is open or closed after an event. */
enum class ViewerEvent { TAP, BACK, VOLUME_UP_LONG }
object ViewerState {
    fun open(image: Boolean): Boolean = image
    fun after(open: Boolean, event: ViewerEvent, moved: Boolean): Boolean = when (event) {
        ViewerEvent.TAP -> open && moved
        else -> false
    }
}

class ZoomView(context: Context, private val bitmap: Bitmap, private val onClose: () -> Unit) : View(context) {
    private var scale = 1f
    private var tx = 0f
    private var ty = 0f
    private var moved = false
    private val paint = Paint(Paint.FILTER_BITMAP_FLAG)
    private val matrix = Matrix()
    private fun fitW() = Math.min(width.toFloat() / bitmap.width, height.toFloat() / bitmap.height) * bitmap.width
    private fun fitH() = Math.min(width.toFloat() / bitmap.width, height.toFloat() / bitmap.height) * bitmap.height
    private fun clampPan() {
        tx = ZoomMath.clampOffset(tx, fitW(), scale, width.toFloat())
        ty = ZoomMath.clampOffset(ty, fitH(), scale, height.toFloat())
    }
    private val scaler = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScaleBegin(detector: ScaleGestureDetector): Boolean { moved = true; return true }
        override fun onScale(detector: ScaleGestureDetector): Boolean {
            val next = ZoomMath.clampScale(scale * detector.scaleFactor)
            val fx = detector.focusX - width / 2f
            val fy = detector.focusY - height / 2f
            tx = ZoomMath.zoomAbout(tx, fx, scale, next); ty = ZoomMath.zoomAbout(ty, fy, scale, next)
            scale = next; clampPan(); invalidate(); return true
        }
    })
    init { scaler.isQuickScaleEnabled = false }
    private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent): Boolean = true
        override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
            if (scaler.isInProgress) return true
            moved = true; tx -= dx; ty -= dy; clampPan(); invalidate(); return true
        }
        override fun onDoubleTap(e: MotionEvent): Boolean {
            val next = ZoomMath.toggle(scale)
            tx = ZoomMath.zoomAbout(tx, e.x - width / 2f, scale, next); ty = ZoomMath.zoomAbout(ty, e.y - height / 2f, scale, next)
            scale = next; clampPan(); invalidate(); return true
        }
        override fun onSingleTapConfirmed(e: MotionEvent): Boolean { if (!ViewerState.after(true, ViewerEvent.TAP, moved)) onClose(); return true }
    })
    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (event.actionMasked == MotionEvent.ACTION_DOWN) moved = false
        scaler.onTouchEvent(event); gestures.onTouchEvent(event); return true
    }
    override fun onDraw(canvas: Canvas) {
        canvas.drawColor(Color.BLACK)
        val base = Math.min(width.toFloat() / bitmap.width, height.toFloat() / bitmap.height)
        matrix.reset()
        matrix.postScale(base * scale, base * scale)
        matrix.postTranslate((width - bitmap.width * base * scale) / 2f + tx, (height - bitmap.height * base * scale) / 2f + ty)
        canvas.drawBitmap(bitmap, matrix, paint)
    }
}

/** Full-screen window above the activity; render() and service updates cannot touch it. */
class ImageViewerDialog(context: Context, bitmap: Bitmap, private val onVolumeUpClose: () -> Unit = {}) : Dialog(context, android.R.style.Theme_Black_NoTitleBar_Fullscreen) {
    init {
        setContentView(ZoomView(context, bitmap) { dismiss() }, ViewGroup.LayoutParams(-1, -1))
        window?.setBackgroundDrawableResource(android.R.color.black)
    }
    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == VolumeNav.UP || keyCode == VolumeNav.DOWN) { if (keyCode == VolumeNav.UP && event.repeatCount == 0) event.startTracking(); return true }
        return super.onKeyDown(keyCode, event)
    }
    override fun onKeyLongPress(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == VolumeNav.UP) { onVolumeUpClose(); dismiss(); return true }
        return super.onKeyLongPress(keyCode, event)
    }
    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean =
        if (keyCode == VolumeNav.UP || keyCode == VolumeNav.DOWN) true else super.onKeyUp(keyCode, event)
}
