package kazuph.ikesu.decisions

import android.Manifest
import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import android.text.Editable
import android.text.TextWatcher
import android.text.util.Linkify
import android.view.KeyEvent
import android.view.Gravity
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ImageView
import android.widget.MediaController
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import android.widget.VideoView
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

class MainActivity : Activity() {
    private val green = 0xFF33FF66.toInt()
    private val dim = 0xFF1E9948.toInt()
    private val warn = 0xFFFFCC33.toInt()
    private data class Draft(var text: String = "", val selected: MutableSet<String> = mutableSetOf(),
                             val images: MutableList<ByteArray> = mutableListOf(), var cursor: Int = 0,
                             var sequence: String? = null, var waitingReconnect: Boolean = false,
                             var uncertain: Boolean = false, var submittedItem: JSONObject? = null)
    private val drafts = mutableMapOf<String, Draft>()
    private var selectedId: String? = null
    private var pendingCamera: Uri? = null
    private var pendingAttachmentId: String? = null
    private var settingsVisible = false
    private var showList = true
    private var listCursor = 0
    private fun dp(value: Int): Int = (value * resources.displayMetrics.density + .5f).toInt()
    private val root by lazy { LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setBackgroundColor(Color.BLACK) } }
    private val header by lazy { LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL } }
    private val layout by lazy { LinearLayout(this).apply { orientation = LinearLayout.VERTICAL } }
    private val footer by lazy { LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL } }
    private val nav = VolumeNav({ move(-1) }, { move(1) }, { confirm() })
    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) { handleUpdate() }
    }
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        root.addView(header)
        root.addView(ScrollView(this).apply { addView(layout) }, LinearLayout.LayoutParams(-1, 0, 1f))
        root.addView(footer)
        setContentView(root.apply {
            setOnApplyWindowInsetsListener { view, insets ->
                val bars = insets.getInsets(android.view.WindowInsets.Type.systemBars() or android.view.WindowInsets.Type.displayCutout())
                view.setPadding(bars.left + dp(20), bars.top, bars.right + dp(20), bars.bottom)
                insets
            }
        })
        registerReceiver(receiver, IntentFilter(DecisionService.CHANGED), RECEIVER_NOT_EXPORTED)
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
        receiveIntent(intent)
        startForegroundService(Intent(this, DecisionService::class.java))
        render()
    }
    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); setIntent(intent); receiveIntent(intent); render() }
    override fun onDestroy() { unregisterReceiver(receiver); super.onDestroy() }
    private fun receiveIntent(intent: Intent) {
        val url = intent.getStringExtra("url")
        val token = intent.getStringExtra("token")
        if (url != null && token != null) {
            try { ConnectionSettings(this).save(url, token); startService(Intent(this, DecisionService::class.java)) }
            catch (error: Exception) { android.util.Log.e("IkesuDecisions", "接続設定を保存できません", error); Toast.makeText(this, error.message, Toast.LENGTH_LONG).show() }
            intent.removeExtra("token")
        }
        intent.getStringExtra("decision")?.let { selectedId = it; showList = false; settingsVisible = false }
    }
    private fun handleUpdate() {
        if (!DecisionState.connected && drafts.values.any { it.sequence != null && !it.waitingReconnect }) {
            drafts.values.filter { it.sequence != null }.forEach { it.waitingReconnect = true }
            Toast.makeText(this, "接続が切れました。再接続後に保存状態を確認してください", Toast.LENGTH_LONG).show()
        }
        DecisionState.result?.let { result ->
            val sequence = result.optString("sequence")
            val draft = drafts.values.firstOrNull { it.sequence == sequence }
            if (draft != null) {
                val status = result.optString("status")
                draft.uncertain = status == "unknown"
                if (!draft.uncertain) draft.sequence = null
                draft.waitingReconnect = false
                val message = when (status) {
                    "answered" -> "回答しました"
                    "answered_elsewhere" -> "ほかの窓口で回答されました"
                    "cancelled" -> "判断待ちは取り消されました"
                    "expired" -> "判断待ちは期限切れです"
                    else -> result.optString("message", "回答できませんでした")
                }
                Toast.makeText(this, message, Toast.LENGTH_LONG).show()
            }
            DecisionState.result = null
        }
        val ids = (0 until DecisionState.decisions.length()).mapNotNull { DecisionState.decisions.optJSONObject(it)?.optString("id") }.toSet()
        listCursor = if (ids.isEmpty()) 0 else listCursor.coerceAtMost(ids.size - 1)
        if (DecisionState.connected) for ((id, draft) in drafts) {
            if (draft.waitingReconnect && draft.sequence != null) {
                if (DecisionState.answer(id, draft.selected.toList(), draft.text, draft.images, draft.sequence!!)) draft.waitingReconnect = false
            }
        }
        drafts.entries.removeAll { it.key !in ids && it.value.sequence == null }
        if (selectedId !in ids && drafts[selectedId]?.sequence == null) { selectedId = null; showList = true }
        if (settingsVisible) {
            (header.getChildAt(0) as? TextView)?.text = "ikesu · ${if (DecisionState.connected) "接続中" else "未接続"}"
        } else render()
    }
    private fun current(): JSONObject? {
        val items = DecisionState.decisions
        for (i in 0 until items.length()) items.optJSONObject(i)?.let { if (it.optString("id") == selectedId) return it }
        return drafts[selectedId]?.takeIf { it.sequence != null }?.submittedItem
    }
    private fun frame(fill: Int = Color.BLACK): GradientDrawable = GradientDrawable().apply {
        setColor(fill); setStroke(dp(2), green); cornerRadius = dp(8).toFloat()
    }
    private fun text(value: String, size: Float = 22f): TextView = TextView(this).apply {
        text = value; textSize = size; setTextColor(green); setLinkTextColor(green)
        gravity = Gravity.START; if (size == 28f) setTypeface(typeface, Typeface.BOLD)
        autoLinkMask = Linkify.WEB_URLS; linksClickable = true; setTextIsSelectable(true)
        movementMethod = android.text.method.LinkMovementMethod.getInstance()
    }
    private fun button(label: String, filled: Boolean = false, action: () -> Unit): Button = Button(this).apply {
        text = label; textSize = 22f; isAllCaps = false; gravity = Gravity.START or Gravity.CENTER_VERTICAL
        setTextColor(if (filled) Color.BLACK else green); background = frame(if (filled) green else Color.BLACK)
        if (filled) setTypeface(typeface, Typeface.BOLD)
        minHeight = dp(64); setPadding(dp(16), 0, dp(16), 0); setOnClickListener { action() }
    }
    private fun block(view: View, margin: Int = 12) {
        layout.addView(view, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(margin) })
    }
    private fun pair(left: View, right: View, target: LinearLayout) {
        target.addView(left, LinearLayout.LayoutParams(0, -2, 1f).apply { marginEnd = dp(6) })
        target.addView(right, LinearLayout.LayoutParams(0, -2, 1f).apply { marginStart = dp(6) })
    }
    private fun input(hintText: String, lines: Int = 1): EditText = EditText(this).apply {
        textSize = 22f; setTextColor(green); setHintTextColor(dim); hint = hintText
        background = frame(); gravity = Gravity.TOP or Gravity.START; minLines = lines
        setPadding(dp(16), dp(16), dp(16), dp(16))
    }
    private fun render() {
        header.removeAllViews(); layout.removeAllViews(); footer.removeAllViews()
        header.addView(text("ikesu · ${if (DecisionState.connected) "接続中" else "未接続"}", 18f), LinearLayout.LayoutParams(0, -2, 1f))
        val headerAction = text(if (settingsVisible || !showList) "一覧へ" else "設定", 18f).apply {
            setTextIsSelectable(false); movementMethod = null
            gravity = Gravity.END; setPadding(dp(8), dp(12), 0, dp(12)); setOnClickListener {
                if (settingsVisible) settingsVisible = false else if (!showList) showList = true else settingsVisible = true
                render()
            }
        }
        header.addView(headerAction)
        if (settingsVisible) { renderSettings(); return }
        val items = DecisionState.decisions
        if (showList) {
            block(text("判断待ち", 28f))
            block(text("${items.length()} 件", 18f))
            for (i in 0 until items.length()) items.optJSONObject(i)?.let { entry ->
                val id = entry.optString("id")
                val row = optionRow(i, entry.optString("title"), entry.optString("location"), i == listCursor, false) {
                    selectedId = id; showList = false; render()
                }
                block(row)
            }
            return
        }
        val item = current()
        if (item == null) { block(text("判断待ちはありません")); return }
        val id = item.optString("id")
        val draft = drafts.getOrPut(id) { Draft() }
        block(text(item.optString("location"), 18f))
        block(text(item.optString("title"), 28f))
        val command = item.optString("approval_command")
        if (command.isNotEmpty()) block(text(command, 20f).apply { setTextColor(Color.RED); typeface = Typeface.MONOSPACE })
        val details = item.optJSONArray("details") ?: JSONArray()
        for (i in 0 until details.length()) details.optJSONObject(i)?.let { block(text("${it.optString("label")}：${it.optString("value")}", 18f)) }
        if (item.optBoolean("machine_down")) block(text("出どころのマシンに接続できないため送れません", 18f).apply { setTextColor(warn) })
        if (item.optString("reauth_url").isNotEmpty()) block(text("SSH の再承認が必要です\n${item.optString("reauth_url")}", 18f).apply { setTextColor(warn) })
        if (item.optString("retry_error").isNotEmpty()) block(text(item.optString("retry_error"), 18f).apply { setTextColor(warn) })
        if (item.optString("body").isNotEmpty()) block(text(item.optString("body")))
        val images = item.optJSONArray("images") ?: JSONArray()
        for (i in 0 until images.length()) addImage(images.optString(i, ""))
        val cards = item.optJSONArray("cards") ?: JSONArray()
        for (i in 0 until cards.length()) cards.optJSONObject(i)?.let { card ->
            addImage(card.optString("image")); block(text("${card.optString("title")}\n${card.optString("url")}"))
        }
        val videos = item.optJSONArray("videos") ?: JSONArray()
        for (i in 0 until videos.length()) videos.optJSONObject(i)?.let { video ->
            DecisionState.videos[video.optString("key")]?.let { file ->
                val player = VideoView(this)
                player.setMediaController(MediaController(this).apply { setAnchorView(player) })
                player.setOnPreparedListener { it.setVolume(0f, 0f) }; player.setVideoPath(file.absolutePath)
                layout.addView(player, LinearLayout.LayoutParams(-1, dp(200)))
            }
        }
        val options = item.optJSONArray("options") ?: JSONArray()
        for (i in 0 until options.length()) {
            val option = options.optJSONObject(i) ?: continue
            val optionId = option.optString("id")
            val description = optionSupplement(option.optString("description"), option.optString("preview"), option.optBoolean("preview_unavailable"))
            block(optionRow(i, option.optString("label"), description, draft.cursor == i, optionId in draft.selected) {
                if (draft.sequence == null) {
                    draft.cursor = i
                    if (item.optBoolean("multi_select")) toggle(draft, optionId) else { draft.selected.clear(); draft.selected.add(optionId) }
                    render()
                }
            })
        }
        if (item.optBoolean("allow_text")) {
            block(text("コメント（任意）", 18f), 4)
            val comment = input("コメントを入力", 3).apply { setText(draft.text); isEnabled = draft.sequence == null }
            comment.addTextChangedListener(object : TextWatcher {
                override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
                override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) { draft.text = s.toString() }
                override fun afterTextChanged(s: Editable?) {}
            })
            block(comment)
            draft.images.forEachIndexed { index, _ -> block(button("添付画像 ${index + 1} ×") { draft.images.removeAt(index); render() }.apply { isEnabled = draft.sequence == null }) }
            val attachments = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            pair(button("写真を選ぶ") { if (draft.images.size < 4) { pendingAttachmentId = id; startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("image/*").addCategory(Intent.CATEGORY_OPENABLE), 2) } }.apply { isEnabled = draft.sequence == null },
                 button("カメラで撮る") { pendingAttachmentId = id; openCamera(draft) }.apply { isEnabled = draft.sequence == null }, attachments)
            block(attachments)
        }
        block(text(deadlineAndCount(item.optString("deadline"), item.optInt("queue_remaining")), 18f))
        footer.setPadding(0, dp(8), 0, dp(8))
        pair(button("あとで") { if (DecisionState.send(JSONObject().put("type", "later").put("id", id))) { showList = true; render() } },
             button(if (draft.uncertain) "保存状態を確認" else if (draft.sequence == null) "回答する" else "送信中", true) { send(item, draft) }.apply { isEnabled = draft.sequence == null || draft.uncertain }, footer)
    }
    private fun optionRow(index: Int, label: String, description: String, focused: Boolean, checked: Boolean, action: () -> Unit): View {
        val color = if (focused) Color.BLACK else green
        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER_VERTICAL; minimumHeight = dp(64)
            background = frame(if (focused) green else Color.BLACK); setPadding(dp(16), dp(12), dp(16), dp(12))
            val heading = text("${if (checked) "✓ " else ""}${index + 1}. $label").apply { setTextIsSelectable(false); movementMethod = null; setTextColor(color) }
            addView(heading)
            if (description.isNotEmpty()) addView(text(description, 18f).apply { setTextIsSelectable(false); movementMethod = null; setTextColor(if (focused) Color.BLACK else dim) })
            setOnClickListener { action() }
        }
    }
    private fun addImage(encoded: String) {
        if (encoded.isEmpty()) return
        try {
            val bytes = android.util.Base64.decode(encoded, android.util.Base64.NO_WRAP)
            val bounds = android.graphics.BitmapFactory.Options().apply { inJustDecodeBounds = true }
            android.graphics.BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
            var sample = 1
            while (bounds.outWidth / sample > 1600 || bounds.outHeight / sample > 1600) sample *= 2
            val bitmap = android.graphics.BitmapFactory.decodeByteArray(bytes, 0, bytes.size, android.graphics.BitmapFactory.Options().apply { inSampleSize = sample }) ?: return
            layout.addView(ImageView(this).apply { setImageBitmap(bitmap); adjustViewBounds = true })
        } catch (_: Exception) { /* Display failures do not prevent answering. */ }
    }
    private fun renderSettings() {
        val current = try { ConnectionSettings(this).load() } catch (_: Exception) { null }
        block(text("接続設定", 28f))
        block(text("接続先", 18f), 4)
        val url = input("ws://接続先:8776").apply { setText(current?.first ?: "") }
        block(url)
        block(text("合言葉", 18f), 4)
        val token = input("合言葉を入力").apply { inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD }
        block(token)
        block(button("保存して接続", true) {
            try { ConnectionSettings(this).save(url.text.toString().trim(), token.text.toString().trim()); settingsVisible = false; startService(Intent(this, DecisionService::class.java)); render() }
            catch (error: Exception) { Toast.makeText(this, error.message, Toast.LENGTH_LONG).show() }
        })
    }
    private fun toggle(draft: Draft, id: String) { if (!draft.selected.add(id)) draft.selected.remove(id) }
    private fun move(delta: Int) {
        if (settingsVisible) return
        if (showList) {
            val count = DecisionState.decisions.length()
            if (count > 0) { listCursor = (listCursor + delta + count) % count; render() }
            return
        }
        val item = current() ?: return
        val count = (item.optJSONArray("options")?.length() ?: 0) + 1
        val draft = drafts.getOrPut(item.optString("id")) { Draft() }
        draft.cursor = (draft.cursor + delta + count) % count
        render()
    }
    private fun confirm() {
        if (settingsVisible) return
        if (showList) {
            DecisionState.decisions.optJSONObject(listCursor)?.let { selectedId = it.optString("id"); showList = false; render() }
            return
        }
        val item = current() ?: return
        val draft = drafts.getOrPut(item.optString("id")) { Draft() }
        if (draft.sequence != null) return
        val options = item.optJSONArray("options") ?: JSONArray()
        if (draft.cursor >= options.length()) { send(item, draft); return }
        val id = options.optJSONObject(draft.cursor)?.optString("id") ?: return
        if (item.optBoolean("multi_select")) { toggle(draft, id); render() }
        else { draft.selected.clear(); draft.selected.add(id); send(item, draft) }
    }
    private fun send(item: JSONObject, draft: Draft) {
        if (draft.sequence != null && !draft.uncertain) return
        val sequence = draft.sequence ?: UUID.randomUUID().toString()
        if (!DecisionState.answer(item.optString("id"), draft.selected.toList(), draft.text, draft.images, sequence)) { Toast.makeText(this, "接続されていません", Toast.LENGTH_LONG).show(); return }
        draft.sequence = sequence
        draft.uncertain = false
        draft.submittedItem = item
        render()
    }
    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (nav.keyDown(keyCode, event.repeatCount == 0)) { event.startTracking(); return true }
        return super.onKeyDown(keyCode, event)
    }
    override fun onKeyLongPress(keyCode: Int, event: KeyEvent): Boolean = nav.keyLongPress(keyCode) || super.onKeyLongPress(keyCode, event)
    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean = nav.keyUp(keyCode) || super.onKeyUp(keyCode, event)
    private fun openCamera(draft: Draft) {
        if (draft.images.size >= 4) return
        pendingCamera = contentResolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, android.content.ContentValues().apply { put(MediaStore.Images.Media.DISPLAY_NAME, "ikesu-${UUID.randomUUID()}.jpg"); put(MediaStore.Images.Media.MIME_TYPE, "image/jpeg") })
        pendingCamera?.let { startActivityForResult(Intent(MediaStore.ACTION_IMAGE_CAPTURE).putExtra(MediaStore.EXTRA_OUTPUT, it).addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION), 3) }
    }
    @Deprecated("Android activity result callback")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        val uri = if (requestCode == 3) pendingCamera else data?.data
        if (resultCode == RESULT_OK && uri != null) {
            try {
                val bytes = contentResolver.openInputStream(uri)?.use { it.readNBytes(20 * 1024 * 1024 + 1) } ?: error("画像を開けません")
                require(bytes.size <= 20 * 1024 * 1024) { "画像は1枚20MBまでです" }
                pendingAttachmentId?.let { drafts[it]?.images?.add(bytes) }
            } catch (error: Exception) { Toast.makeText(this, error.message, Toast.LENGTH_LONG).show() }
        }
        if (requestCode == 3 && uri != null) contentResolver.delete(uri, null, null)
        pendingCamera = null; pendingAttachmentId = null; render()
    }
}
