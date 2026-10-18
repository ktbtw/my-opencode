package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import androidx.core.content.ContextCompat

/**
 * 语音输入封装：把 SpeechRecognizer 的回调收敛成 partial / final / error 三个口子。
 * 由悬浮层服务持有，生命周期跟随服务。
 */
internal class VoiceInput(private val context: Context) {

    var onPartial: ((String) -> Unit)? = null
    var onFinal: ((String) -> Unit)? = null
    var onError: ((String) -> Unit)? = null
    var onEnd: (() -> Unit)? = null

    private var recognizer: SpeechRecognizer? = null
    private var running = false

    fun hasPermission(): Boolean = ContextCompat.checkSelfPermission(
        context,
        android.Manifest.permission.RECORD_AUDIO,
    ) == PackageManager.PERMISSION_GRANTED

    fun available(): Boolean = SpeechRecognizer.isRecognitionAvailable(context)

    fun start() {
        if (running) return
        if (!hasPermission()) {
            onError?.invoke("需要麦克风权限")
            return
        }
        if (!available()) {
            onError?.invoke("当前设备没有可用的语音识别")
            return
        }
        val engine = SpeechRecognizer.createSpeechRecognizer(context)
        engine.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) = Unit
            override fun onBeginningOfSpeech() = Unit
            override fun onRmsChanged(rmsdB: Float) = Unit
            override fun onBufferReceived(buffer: ByteArray?) = Unit
            override fun onEndOfSpeech() = Unit

            override fun onPartialResults(partialResults: Bundle?) {
                firstResult(partialResults)?.let { onPartial?.invoke(it) }
            }

            override fun onResults(results: Bundle?) {
                running = false
                firstResult(results)?.let { onFinal?.invoke(it) } ?: onError?.invoke("没有听清")
                onEnd?.invoke()
            }

            override fun onError(error: Int) {
                running = false
                onError?.invoke(describe(error))
                onEnd?.invoke()
            }

            override fun onEvent(eventType: Int, params: Bundle?) = Unit
        })
        recognizer = engine
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(
                RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
            )
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "zh-CN")
            putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1)
        }
        running = true
        runCatching { engine.startListening(intent) }.onFailure {
            running = false
            onError?.invoke("语音识别启动失败")
        }
    }

    fun stop() {
        if (!running) return
        running = false
        runCatching { recognizer?.stopListening() }
    }

    fun release() {
        running = false
        runCatching { recognizer?.destroy() }
        recognizer = null
    }

    private fun firstResult(bundle: Bundle?): String? =
        bundle?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
            ?.firstOrNull()
            ?.trim()
            ?.takeIf { it.isNotEmpty() }

    private fun describe(error: Int): String = when (error) {
        SpeechRecognizer.ERROR_AUDIO -> "录音出错"
        SpeechRecognizer.ERROR_CLIENT -> "识别被中断"
        SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> "需要麦克风权限"
        SpeechRecognizer.ERROR_NETWORK, SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> "网络异常"
        SpeechRecognizer.ERROR_NO_MATCH -> "没有听清"
        SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> "识别忙，稍后再试"
        SpeechRecognizer.ERROR_SERVER -> "识别服务异常"
        SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> "没有检测到说话"
        else -> "识别失败"
    }
}
