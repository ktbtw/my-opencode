package com.chatcodex.chat_codex_app.overlay

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.PixelFormat
import android.hardware.display.DisplayManager
import android.media.Image
import android.media.ImageReader
import android.media.projection.MediaProjectionManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.chinese.ChineseTextRecognizerOptions
import java.nio.ByteBuffer

private const val OCR_TAG = "ScreenOcr"

/** 截屏授权结果与 OCR 回调的进程内总线。 */
internal object ScreenOcrBus {
    var resultCode = 0
    var resultData: Intent? = null

    var onText: ((String) -> Unit)? = null
    var onError: ((String) -> Unit)? = null
    var onStart: (() -> Unit)? = null

    fun clearConsent() {
        resultCode = 0
        resultData = null
    }

    fun hasConsent(): Boolean = resultCode != 0 && resultData != null
}

/** 透明授权页：只负责弹出系统录屏确认，拿结果后立刻结束。 */
class ScreenCaptureConsentActivity : Activity() {

    override fun onCreate(savedInstanceState: android.os.Bundle?) {
        super.onCreate(savedInstanceState)
        val manager = getSystemService(MediaProjectionManager::class.java)
        if (manager == null) {
            ScreenOcrBus.onError?.invoke("当前设备不支持屏幕识别")
            finish()
            return
        }
        runCatching {
            startActivityForResult(manager.createScreenCaptureIntent(), REQUEST_CONSENT)
        }.onFailure {
            ScreenOcrBus.onError?.invoke("无法打开录屏授权")
            finish()
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode != REQUEST_CONSENT) {
            super.onActivityResult(requestCode, resultCode, data)
            return
        }
        if (resultCode == RESULT_OK && data != null) {
            ScreenOcrBus.resultCode = resultCode
            ScreenOcrBus.resultData = data
            ScreenCaptureService.start(this)
        } else {
            ScreenOcrBus.onError?.invoke("已取消屏幕识别")
        }
        finish()
    }

    companion object {
        private const val REQUEST_CONSENT = 7301

        fun intent(context: Context): Intent =
            Intent(context, ScreenCaptureConsentActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
}

/** 一次性截屏 + 中文 OCR。用前台服务承载 mediaProjection 类型。 */
class ScreenCaptureService : android.app.Service() {

    private val handler = Handler(Looper.getMainLooper())

    override fun onBind(intent: Intent?) = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForegroundCompat()
        runCatching { captureOnce() }.onFailure {
            Log.w(OCR_TAG, "capture failed", it)
            ScreenOcrBus.onError?.invoke("截屏失败")
            stopSelf()
        }
        return START_NOT_STICKY
    }

    private fun startForegroundCompat() {
        val notification = androidx.core.app.NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("屏幕识别")
            .setContentText("正在读取当前画面…")
            .setSmallIcon(android.R.drawable.ic_menu_view)
            .setOngoing(true)
            .build()
        val manager = getSystemService(android.app.NotificationManager::class.java)
        if (manager != null) {
            val channel = android.app.NotificationChannel(
                CHANNEL_ID,
                "屏幕识别",
                android.app.NotificationManager.IMPORTANCE_LOW,
            )
            manager.createNotificationChannel(channel)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIFICATION_ID,
                notification,
                android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION,
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    private fun captureOnce() {
        if (!ScreenOcrBus.hasConsent()) {
            ScreenOcrBus.onError?.invoke("缺少屏幕授权")
            stopSelf()
            return
        }
        val manager = getSystemService(MediaProjectionManager::class.java)
        val projection = manager?.getMediaProjection(
            ScreenOcrBus.resultCode,
            ScreenOcrBus.resultData!!,
        ) ?: run {
            ScreenOcrBus.onError?.invoke("屏幕授权已失效，请重新识别")
            stopSelf()
            return
        }
        val metrics = resources.displayMetrics
        val width = metrics.widthPixels
        val height = metrics.heightPixels
        val reader = ImageReader.newInstance(width, height, PixelFormat.RGBA_8888, 2)
        val display = projection.createVirtualDisplay(
            "screen-ocr",
            width,
            height,
            metrics.densityDpi,
            DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR,
            reader.surface,
            null,
            handler,
        ) ?: run {
            ScreenOcrBus.onError?.invoke("无法创建屏幕镜像")
            projection.stop()
            reader.close()
            stopSelf()
            return
        }
        reader.setOnImageAvailableListener({ source ->
            val image = runCatching { source.acquireLatestImage() }.getOrNull() ?: return@setOnImageAvailableListener
            val bitmap = runCatching { image.toBitmap(width, height) }.getOrNull()
            runCatching { image.close() }
            runCatching { display.release() }
            runCatching { projection.stop() }
            runCatching { source.close() }
            if (bitmap == null) {
                ScreenOcrBus.onError?.invoke("画面读取失败")
                stopSelf()
                return@setOnImageAvailableListener
            }
            recognize(bitmap)
        }, handler)
    }

    private fun recognize(bitmap: Bitmap) {
        recognizeBitmap(
            bitmap = bitmap,
            onText = { text -> ScreenOcrBus.onText?.invoke(text) },
            onError = { message -> ScreenOcrBus.onError?.invoke(message) },
            onDone = { stopSelf() },
        )
    }

    override fun onDestroy() {
        runCatching {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                stopForeground(STOP_FOREGROUND_REMOVE)
            } else {
                @Suppress("DEPRECATION")
                stopForeground(true)
            }
        }
        super.onDestroy()
    }

    companion object {
        private const val CHANNEL_ID = "screen_ocr"
        private const val NOTIFICATION_ID = 4311

        fun start(context: Context) {
            val intent = Intent(context, ScreenCaptureService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }
    }
}

/** ImageReader 的 RGBA 帧转 Bitmap，处理行跨距填充。 */
private fun Image.toBitmap(width: Int, height: Int): Bitmap {
    val plane = planes[0]
    val buffer: ByteBuffer = plane.buffer
    val pixelStride = plane.pixelStride
    val rowStride = plane.rowStride
    val rowPadding = rowStride - pixelStride * width
    val bitmapWidth = width + rowPadding / pixelStride
    val bitmap = Bitmap.createBitmap(bitmapWidth, height, Bitmap.Config.ARGB_8888)
    bitmap.copyPixelsFromBuffer(buffer)
    if (bitmapWidth == width) return bitmap
    val cropped = Bitmap.createBitmap(bitmap, 0, 0, width, height)
    bitmap.recycle()
    return cropped
}

/**
 * 通用 OCR：成功回调整段文本，失败回调中文原因，完成后回调。
 * 首次使用需要下载中文模型，遇到下载中的提示会自动重试。
 */
internal fun recognizeBitmap(
    bitmap: Bitmap,
    onText: (String) -> Unit,
    onError: (String) -> Unit,
    onDone: () -> Unit,
    attempt: Int = 0,
    onDownloading: (() -> Unit)? = null,
) {
    val recognizer = TextRecognition.getClient(ChineseTextRecognizerOptions.Builder().build())
    recognizer.process(InputImage.fromBitmap(bitmap, 0))
        .addOnSuccessListener { result ->
            val text = result.text.trim()
            if (text.isEmpty()) onError("画面里没有识别到文字") else onText(text)
        }
        .addOnFailureListener { error ->
            val message = error.message.orEmpty()
            val downloading = message.contains("download", ignoreCase = true) ||
                message.contains("Waiting for the text", ignoreCase = true)
            if (downloading && attempt < 6) {
                onDownloading?.invoke()
                android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({
                    recognizeBitmap(bitmap, onText, onError, onDone, attempt + 1, onDownloading)
                }, 4000)
                return@addOnFailureListener
            }
            Log.w(OCR_TAG, "ocr failed", error)
            onError(
                if (downloading) "识别模型下载未完成，请联网后重试"
                else "识别失败：${message.ifBlank { "未知错误" }}",
            )
        }
        .addOnCompleteListener {
            if (!it.isSuccessful && attempt < 6) return@addOnCompleteListener
            runCatching { recognizer.close() }
            runCatching { bitmap.recycle() }
            onDone()
        }
}
