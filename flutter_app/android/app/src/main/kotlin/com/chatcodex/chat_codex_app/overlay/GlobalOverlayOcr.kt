package com.chatcodex.chat_codex_app.overlay

import com.chatcodex.chat_codex_app.overlay.compose.FloatingMode

/** 进入屏幕识别：切模式 + 弹系统录屏授权。 */
internal fun GlobalOverlayService.startScreenOcr() {
    val state = ensureComposeState()
    state.ocrBusy = true
    state.ocrText = ""
    state.ocrError = ""
    // 每次识屏都重新走一次系统授权：Android 14 起同一份授权只能消费一次。
    ScreenOcrBus.clearConsent()
    applyComposeMode(FloatingMode.SCREEN_OCR)

    ScreenOcrBus.onText = { text ->
        val current = composeUiState
        if (current != null) {
            current.ocrText = text
            current.ocrBusy = false
        }
    }
    ScreenOcrBus.onError = { message ->
        val current = composeUiState
        if (current != null) {
            current.ocrError = message
            current.ocrBusy = false
        }
    }
    runCatching {
        startActivity(ScreenCaptureConsentActivity.intent(this))
    }.onFailure {
        state.ocrError = "无法启动屏幕识别"
        state.ocrBusy = false
    }
}

/** 关闭屏幕识别面板，回到悬浮窗。 */
internal fun GlobalOverlayService.closeScreenOcr() {
    val state = composeUiState ?: return
    state.ocrBusy = false
    applyComposeMode(FloatingMode.WINDOW)
}

/** 把识别结果填进输入框，回到悬浮窗准备发送。 */
internal fun GlobalOverlayService.sendScreenOcrText() {
    val state = composeUiState ?: return
    if (state.ocrText.isNotBlank()) {
        state.input = state.ocrText
    }
    state.ocrBusy = false
    applyComposeMode(FloatingMode.WINDOW)
}
