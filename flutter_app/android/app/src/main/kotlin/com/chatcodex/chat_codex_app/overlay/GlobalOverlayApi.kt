package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL

/**
 * HTTP 访问层：统一的 JSON 请求、底层请求执行、401 时用账号密码自动刷新 Token，
 * 以及错误信息的中文化。
 */

/** 发起请求并把响应体解析为 JSON（失败返回 null）。 */
internal fun GlobalOverlayService.apiRequest(path: String, method: String = "GET", body: JSONObject? = null): JSONObject? =
    runCatching { JSONObject(apiRaw(path, method, body) ?: return null) }.getOrNull()

/** 发起请求并返回原始响应体字符串（失败返回 null，错误信息写入 lastApiError）。 */
internal fun GlobalOverlayService.apiRaw(path: String, method: String = "GET", body: JSONObject? = null): String? {
    val prefs = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
    val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
    var token = prefs.getString("flutter.access_token", "").orEmpty()
    lastApiError = ""
    if (base.isBlank() || token.isBlank()) {
        lastApiError = "登录状态已失效，请重新打开应用"
        return null
    }
    var result = performApiRequest(base, path, method, body, token)
    if (result.statusCode == HttpURLConnection.HTTP_UNAUTHORIZED && refreshAccessToken(token)) {
        token = prefs.getString("flutter.access_token", "").orEmpty()
        result = performApiRequest(base, path, method, body, token)
    }
    if (result.statusCode in 200..299) return result.body
    lastApiError = parseApiError(result)
    return null
}

/** 执行一次 HTTP 请求，返回状态码与响应体。 */
internal fun GlobalOverlayService.performApiRequest(
    base: String,
    path: String,
    method: String,
    body: JSONObject?,
    token: String,
): HttpResult {
    val connection = (URL("$base$path").openConnection() as HttpURLConnection).apply {
        requestMethod = method
        connectTimeout = 8000
        readTimeout = 12000
        setRequestProperty("Authorization", "Bearer $token")
        setRequestProperty("Content-Type", "application/json")
        doInput = true
        if (body != null) {
            doOutput = true
            outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
        }
    }
    return try {
        val statusCode = connection.responseCode
        val stream = if (statusCode in 200..299) connection.inputStream else connection.errorStream
        HttpResult(statusCode, stream?.use { BufferedReader(InputStreamReader(it, Charsets.UTF_8)).readText() }.orEmpty())
    } finally {
        connection.disconnect()
    }
}

/** 用本地保存的账号密码重新登录以刷新 Token（并发安全）。 */
internal fun GlobalOverlayService.refreshAccessToken(staleToken: String): Boolean = synchronized(tokenRefreshLock) {
    val prefs = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
    val currentToken = prefs.getString("flutter.access_token", "").orEmpty()
    if (currentToken.isNotBlank() && currentToken != staleToken) return@synchronized true
    val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
    val username = prefs.getString("flutter.username", "").orEmpty()
    val password = prefs.getString("flutter.password", "").orEmpty()
    if (base.isBlank() || username.isBlank() || password.isBlank()) return@synchronized false
    val connection = (URL("$base/api/auth/login").openConnection() as HttpURLConnection).apply {
        requestMethod = "POST"
        connectTimeout = 8000
        readTimeout = 12000
        setRequestProperty("Content-Type", "application/json")
        doInput = true
        doOutput = true
    }
    try {
        val payload = JSONObject().put("username", username).put("password", password)
        connection.outputStream.use { it.write(payload.toString().toByteArray(Charsets.UTF_8)) }
        if (connection.responseCode !in 200..299) return@synchronized false
        val raw = connection.inputStream.use { BufferedReader(InputStreamReader(it, Charsets.UTF_8)).readText() }
        val token = runCatching { JSONObject(raw).optString("access_token") }.getOrDefault("")
        if (token.isBlank()) return@synchronized false
        prefs.edit().putString("flutter.access_token", token).commit()
    } finally {
        connection.disconnect()
    }
}

/** 把 HTTP 错误响应转换成中文提示。 */
internal fun GlobalOverlayService.parseApiError(result: HttpResult): String {
    val detail = runCatching { JSONObject(result.body).optString("error") }.getOrDefault("")
    return when {
        detail.isNotBlank() -> "发送失败：$detail"
        result.statusCode == HttpURLConnection.HTTP_UNAUTHORIZED -> "登录状态已失效，请重新打开应用"
        result.statusCode > 0 -> "发送失败（HTTP ${result.statusCode}）"
        else -> "发送失败，请稍后重试"
    }
}
