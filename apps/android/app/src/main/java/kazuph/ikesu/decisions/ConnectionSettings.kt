package kazuph.ikesu.decisions

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class ConnectionSettings(context: Context) {
    private val preferences = context.getSharedPreferences("connection", Context.MODE_PRIVATE)
    private val alias = "ikesu-decisions-token"
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(alias, null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").run {
            init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
            generateKey()
        }
    }
    fun save(url: String, token: String) {
        require(token.matches(Regex("[0-9a-f]{64}"))) { "合言葉が不正です" }
        val target = runCatching { java.net.URI(url) }.getOrNull()
        require(target != null && target.scheme in listOf("ws", "wss") && target.host != null &&
            target.userInfo == null && target.query == null && target.fragment == null &&
            (target.path.isNullOrEmpty() || target.path == "/")) { "接続先は ws:// または wss:// のアドレスを指定してください" }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val ciphertext = cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        preferences.edit().putString("url", url)
            .putString("iv", Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
            .putString("token", Base64.encodeToString(ciphertext, Base64.NO_WRAP)).apply()
    }
    fun load(): Pair<String, String>? {
        val url = preferences.getString("url", null) ?: return null
        val iv = preferences.getString("iv", null) ?: return null
        val token = preferences.getString("token", null) ?: return null
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(iv, Base64.NO_WRAP)))
        return url to String(cipher.doFinal(Base64.decode(token, Base64.NO_WRAP)), Charsets.UTF_8)
    }
}
