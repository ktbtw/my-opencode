package com.chatcodex.chat_codex_app.overlay

import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.view.WindowManager
import com.chatcodex.chat_codex_app.overlay.compose.FloatingMode

/** 复用（或创建）语音识别引擎，回调只接一次。 */
private fun GlobalOverlayService.ensureVoiceInput(): VoiceInput =
    voiceInput ?: VoiceInput(this).also { engine ->
        engine.onPartial = { text ->
            // 部分识别引擎只给最终结果；给 partial 时同时同步到输入框，切换回去也能看到。
            val current = composeUiState
            if (current != null) {
                current.voiceText = text
            }
        }
        engine.onFinal = { text ->
            val current = composeUiState
            if (current != null) {
                current.voiceText = text
                current.input = text
                current.voiceActive = false
            }
            // 识别完成回到悬浮窗，文本已填进输入框，用户确认后再发送。
            applyComposeMode(FloatingMode.WINDOW)
        }
        engine.onError = { message -> composeUiState?.voiceError = message }
        engine.onEnd = { composeUiState?.voiceActive = false }
        voiceInput = engine
    }

/**
 * 进入语音球：先补麦克风运行时权限，再切模式并启动识别。
 *
 * 以前这里直接 start()，没权限时 VoiceInput 只把「需要麦克风权限」写进错误文案，
 * 系统权限框从来没弹过；现在缺权限就先走透明授权页，拿到结果再重试。
 */
internal fun GlobalOverlayService.startVoiceBall() {
    val voice = ensureVoiceInput()
    if (!voice.hasPermission()) {
        MicPermissionBus.onResult = { granted ->
            handler.post {
                if (destroyed) return@post
                if (granted) {
                    startVoiceBall()
                } else {
                    val state = ensureComposeState()
                    state.voiceActive = false
                    state.voiceError = "需要麦克风权限"
                    promptMicrophonePermission()
                }
            }
        }
        val launched = runCatching {
            startActivity(OverlayMicPermissionActivity.intent(this))
        }.isSuccess
        if (!launched) {
            MicPermissionBus.onResult = null
            toast("无法申请麦克风权限")
        }
        return
    }
    val state = ensureComposeState()
    state.voiceError = ""
    state.voiceText = ""
    state.voiceActive = true
    applyComposeMode(FloatingMode.VOICE_BALL)
    voice.start()
}

/** 退出语音球：停止识别并回到悬浮窗。 */
internal fun GlobalOverlayService.stopVoiceBall() {
    voiceInput?.stop()
    val state = composeUiState ?: return
    state.voiceActive = false
    applyComposeMode(FloatingMode.WINDOW)
}

/**
 * 权限被拒后的确认框。
 *
 * 以前这里直接跳应用详情页，用户会觉得莫名其妙；改成先问一句，愿意再去设置。
 * 系统不会再弹原生授权框的情况（拒绝过且不再询问）只能靠设置页打开。
 */
private fun GlobalOverlayService.promptMicrophonePermission() {
    handler.post {
        if (destroyed) return@post
        val dialog = runCatching {
            AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Light_Dialog_Alert)
                .setTitle("需要麦克风权限")
                .setMessage("语音输入要用麦克风。去系统设置里允许「录音」后回来再点一次。")
                .setPositiveButton("去设置") { _, _ -> openAppPermissionSettings() }
                .setNegativeButton("取消", null)
                .create()
        }.getOrNull() ?: return@post
        dialog.window?.apply {
            setType(
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
                } else {
                    @Suppress("DEPRECATION")
                    WindowManager.LayoutParams.TYPE_PHONE
                },
            )
            setDimAmount(0.4f)
        }
        runCatching { dialog.show() }.onFailure { toast("请到系统设置开启麦克风权限") }
    }
}

/** 打开本应用的系统详情页（权限在那里改）。 */
private fun GlobalOverlayService.openAppPermissionSettings() {
    runCatching {
        startActivity(
            Intent(
                Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                Uri.parse("package:$packageName"),
            ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }
}
