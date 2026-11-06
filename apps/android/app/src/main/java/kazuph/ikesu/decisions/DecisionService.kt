package kazuph.ikesu.decisions

import kazuph.ikesu.R

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.widget.RemoteViews
import android.net.ConnectivityManager
import android.net.Network
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit
import java.io.File
import java.io.FileOutputStream
import android.util.Base64

object DecisionState : AnswerChannel {
    @Volatile var decisions = JSONArray()
    @Volatile var connected = false
    @Volatile var result: JSONObject? = null
    @Volatile var socket: WebSocket? = null
    val videos = mutableMapOf<String, File>()
    fun send(value: JSONObject): Boolean = socket?.send(value.toString()) == true
    override fun answer(decisionId: String, optionIds: List<String>, text: String, images: List<ByteArray>, sequence: String): Boolean =
        send(JSONObject().put("type", "answer").put("id", decisionId).put("sequence", sequence)
            .put("options", JSONArray(optionIds)).put("text", text)
            .put("images", JSONArray(images.map { Base64.encodeToString(it, Base64.NO_WRAP) })))
}

class DecisionService : Service() {
    companion object {
        const val CHANGED = "kazuph.ikesu.CHANGED"
        private const val CONNECTION = "connection"
        private const val QUESTIONS = "questions"
        private const val FOREGROUND_ID = 1
        private const val RETRY_MS = 2000L
    }
    private val handler = Handler(Looper.getMainLooper())
    private val client = OkHttpClient.Builder().pingInterval(20, TimeUnit.SECONDS).build()
    private val notificationIds = mutableMapOf<String, Int>()
    private var nextNotification = 2
    private var generation = 0
    private val receiving = mutableMapOf<String, Pair<FileOutputStream, Int>>()
    private val manager by lazy { getSystemService(NotificationManager::class.java) }
    private val connectivity by lazy { getSystemService(ConnectivityManager::class.java) }
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) { handler.post { connect() } }
    }

    override fun onCreate() {
        super.onCreate()
        manager.createNotificationChannel(NotificationChannel(CONNECTION, "接続状態", NotificationManager.IMPORTANCE_LOW))
        manager.createNotificationChannel(NotificationChannel(QUESTIONS, "判断待ち", NotificationManager.IMPORTANCE_HIGH))
        startForeground(FOREGROUND_ID, connectionNotification())
        connectivity.registerDefaultNetworkCallback(networkCallback)
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        connect()
        return START_STICKY
    }
    private fun changed() {
        manager.notify(FOREGROUND_ID, connectionNotification())
        sendBroadcast(Intent(CHANGED).setPackage(packageName))
    }
    private fun connectionNotification(): Notification = Notification.Builder(this, CONNECTION)
        .setSmallIcon(R.drawable.ic_notification).setContentTitle("ikesu")
        .setContentText("${if (DecisionState.connected) "接続中" else "未接続"} · 未回答 ${DecisionState.decisions.length()}件")
        .setColor(0xFF33FF66.toInt()).setCustomContentView(compactNotification("ikesu · ${if (DecisionState.connected) "接続中" else "未接続"} · 未回答 ${DecisionState.decisions.length()}件"))
        .setCustomBigContentView(notificationView("ikesu", "${if (DecisionState.connected) "接続中" else "未接続"} · 未回答 ${DecisionState.decisions.length()}件"))
        .setStyle(Notification.DecoratedCustomViewStyle())
        .setOngoing(true).setContentIntent(PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)).build()
    private fun notificationView(title: String, detail: String): RemoteViews = RemoteViews(packageName, R.layout.notification).apply {
        setTextViewText(R.id.notification_title, title)
        setTextViewText(R.id.notification_text, detail)
    }
    private fun compactNotification(text: String): RemoteViews = RemoteViews(packageName, R.layout.notification_compact).apply {
        setTextViewText(R.id.notification_compact, text)
    }
    private fun connect() {
        val thisGeneration = ++generation
        DecisionState.socket?.cancel()
        DecisionState.socket = null
        DecisionState.connected = false
        changed()
        val settings = try { ConnectionSettings(this).load() } catch (_: Exception) { null } ?: return
        val request = try { Request.Builder().url(settings.first).build() } catch (_: IllegalArgumentException) { return }
        client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                if (thisGeneration != generation) { webSocket.cancel(); return }
                android.util.Log.i("IkesuDecisions", "bridge 接続確立")
                webSocket.send(JSONObject().put("type", "auth").put("token", settings.second).toString())
            }
            override fun onMessage(webSocket: WebSocket, text: String) {
                if (thisGeneration != generation) return
                val message = try { JSONObject(text) } catch (_: Exception) { return }
                handler.post {
                    if (thisGeneration != generation) return@post
                    when (message.optString("type")) {
                        "snapshot" -> {
                            android.util.Log.i("IkesuDecisions", "判断待ち一覧を受信")
                            DecisionState.socket = webSocket
                            DecisionState.connected = true
                            updateDecisions(message.optJSONArray("decisions") ?: JSONArray())
                        }
                        "answer_result", "error" -> { DecisionState.result = message; changed() }
                        "video_chunk" -> receiveVideo(message)
                        "video_end" -> finishVideo(message)
                    }
                }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                android.util.Log.w("IkesuDecisions", "bridge 接続失敗: ${t.javaClass.simpleName}: ${t.message}")
                disconnected(thisGeneration)
            }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                android.util.Log.w("IkesuDecisions", "bridge 接続終了: $code")
                disconnected(thisGeneration)
            }
        })
    }
    private fun disconnected(previous: Int) {
        handler.post {
            if (previous != generation) return@post
            DecisionState.socket = null
            DecisionState.connected = false
            receiving.forEach { (key, value) -> value.first.close(); File(cacheDir, "video-$key.partial").delete() }
            receiving.clear()
            changed()
            handler.postDelayed({ if (previous == generation) connect() }, RETRY_MS)
        }
    }
    private fun updateDecisions(items: JSONArray) {
        val previous = DecisionState.decisions
        val old = (0 until previous.length()).mapNotNull { previous.optJSONObject(it) }.associate { it.optString("id") to it.optBoolean("notify") }
        val current = (0 until items.length()).mapNotNull { items.optJSONObject(it)?.optString("id") }.toSet()
        (old.keys - current).forEach { id -> notificationIds.remove(id)?.let(manager::cancel) }
        DecisionState.decisions = items
        val activeVideos = (0 until items.length()).flatMap { index ->
            val videos = items.optJSONObject(index)?.optJSONArray("videos") ?: JSONArray()
            (0 until videos.length()).mapNotNull { videos.optJSONObject(it)?.optString("key") }
        }.toSet()
        (DecisionState.videos.keys - activeVideos).forEach { key -> DecisionState.videos.remove(key)?.delete() }
        (receiving.keys - activeVideos).forEach { key -> receiving.remove(key)?.first?.close(); File(cacheDir, "video-$key.partial").delete() }
        for (index in 0 until items.length()) {
            val item = items.optJSONObject(index) ?: continue
            val id = item.optString("id")
            if (id.isEmpty()) continue
            if (!item.optBoolean("notify")) { notificationIds[id]?.let(manager::cancel); continue }
            if (old[id] == true) continue
            val number = notificationIds.getOrPut(id) { nextNotification++ }
            val intent = Intent(this, MainActivity::class.java).putExtra("decision", id).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)
            val pending = PendingIntent.getActivity(this, number, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
            manager.notify(number, Notification.Builder(this, QUESTIONS).setSmallIcon(R.drawable.ic_notification)
                .setContentTitle(item.optString("title", "判断待ち"))
                .setContentText(item.optString("location", "判断を待っています"))
                .setColor(0xFF33FF66.toInt()).setCustomContentView(compactNotification(item.optString("title", "判断待ち")))
                .setCustomBigContentView(notificationView(item.optString("title", "判断待ち"), item.optString("location", "判断を待っています")))
                .setStyle(Notification.DecoratedCustomViewStyle())
                .setContentIntent(pending).setAutoCancel(true).build())
        }
        changed()
    }
    private fun receiveVideo(message: JSONObject) {
        val key = message.optString("key")
        val id = message.optString("id")
        val allowed = (0 until DecisionState.decisions.length()).mapNotNull { DecisionState.decisions.optJSONObject(it) }
            .any { item -> item.optString("id") == id && (item.optJSONArray("videos") ?: JSONArray()).let { videos ->
                (0 until videos.length()).any { videos.optJSONObject(it)?.optString("key") == key }
            } }
        if (!allowed || !key.matches(Regex("[0-9a-f-]{36}"))) return
        try {
            val index = message.getInt("index")
            val state = receiving[key] ?: if (index == 0) FileOutputStream(File(cacheDir, "video-$key.partial")) to 0 else return
            if (index != state.second) { state.first.close(); receiving.remove(key); return }
            state.first.write(Base64.decode(message.getString("data"), Base64.NO_WRAP))
            receiving[key] = state.first to index + 1
        } catch (_: Exception) { receiving.remove(key)?.first?.close(); File(cacheDir, "video-$key.partial").delete() }
    }
    private fun finishVideo(message: JSONObject) {
        val key = message.optString("key")
        val state = receiving.remove(key) ?: return
        state.first.close()
        val file = File(cacheDir, "video-$key")
        if (!File(cacheDir, "video-$key.partial").renameTo(file)) return
        DecisionState.videos[key] = file
        changed()
    }
    override fun onDestroy() {
        generation++
        connectivity.unregisterNetworkCallback(networkCallback)
        handler.removeCallbacksAndMessages(null)
        receiving.forEach { (key, value) -> value.first.close(); File(cacheDir, "video-$key.partial").delete() }
        receiving.clear()
        DecisionState.socket?.close(1000, "")
        DecisionState.socket = null
        DecisionState.connected = false
        super.onDestroy()
    }
    override fun onBind(intent: Intent?): IBinder? = null
}
