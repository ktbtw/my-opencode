package com.chatcodex.chat_codex_app.overlay.compose

/** 悬浮助手的工作模式，对齐 Operit 的 FloatingMode。 */
enum class FloatingMode {
    BALL,
    /** 语音球：放大球体进入聆听态。 */
    VOICE_BALL,
    WINDOW,
    FULLSCREEN,
    RESULT_DISPLAY,
}
