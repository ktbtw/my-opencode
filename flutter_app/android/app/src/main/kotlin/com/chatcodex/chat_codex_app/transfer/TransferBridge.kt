package com.chatcodex.chat_codex_app.transfer

import android.content.Context
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/** Dart 侧控制传输前台服务的桥接。 */
class TransferBridge(private val context: Context) : MethodChannel.MethodCallHandler {
    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "start" -> {
                val title = call.argument<String>("title") ?: "正在传输文件"
                val text = call.argument<String>("text") ?: "保持后台运行"
                try {
                    TransferForegroundService.start(context, title, text)
                    result.success(true)
                } catch (error: Exception) {
                    result.error("start_failed", error.message, null)
                }
            }
            "update" -> {
                val title = call.argument<String>("title") ?: "正在传输文件"
                val text = call.argument<String>("text") ?: "保持后台运行"
                try {
                    TransferForegroundService.start(context, title, text)
                    result.success(true)
                } catch (error: Exception) {
                    result.error("update_failed", error.message, null)
                }
            }
            "stop" -> {
                try {
                    TransferForegroundService.stop(context)
                    result.success(true)
                } catch (error: Exception) {
                    result.error("stop_failed", error.message, null)
                }
            }
            else -> result.notImplemented()
        }
    }
}
