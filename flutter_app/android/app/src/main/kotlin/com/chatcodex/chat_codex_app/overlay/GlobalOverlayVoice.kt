package com.chatcodex.chat_codex_app.overlay

import com.chatcodex.chat_codex_app.overlay.compose.FloatingMode

/** 进入语音球：切模式 + 启动识别。 */
internal fun GlobalOverlayService.startVoiceBall() {
    val state = ensureComposeState()
    state.voiceError = ""
    state.voiceText = ""
    state.voiceActive = true
    applyComposeMode(FloatingMode.VOICE_BALL)

    val voice = voiceInput ?: VoiceInput(this).also { engine ->
        engine.onPartial = { text -> composeUiState?.voiceText = text }
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
    voice.start()
}

/** 退出语音球：停止识别并回到悬浮窗。 */
internal fun GlobalOverlayService.stopVoiceBall() {
    voiceInput?.stop()
    val state = composeUiState ?: return
    state.voiceActive = false
    applyComposeMode(FloatingMode.WINDOW)
}
