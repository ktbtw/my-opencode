package com.chatcodex.chat_codex_app.transfer

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import com.chatcodex.chat_codex_app.MainActivity

/**
 * 传输前台服务：上传/下载期间常驻，避免 App 退到后台后系统暂停网络或回收进程。
 * 只负责保活与展示进度通知，真正的分块传输仍由 Dart 侧执行。
 */
class TransferForegroundService : Service() {
    companion object {
        private const val CHANNEL_ID = "chat_codex_transfer"
        private const val NOTIFICATION_ID = 4211
        const val EXTRA_TITLE = "title"
        const val EXTRA_TEXT = "text"
        const val EXTRA_PROGRESS = "progress"

        fun start(context: Context, title: String, text: String) {
            val intent = Intent(context, TransferForegroundService::class.java).apply {
                putExtra(EXTRA_TITLE, title)
                putExtra(EXTRA_TEXT, text)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        /** 只更新通知内容/进度，不重启服务。 */
        fun update(context: Context, title: String, text: String, progress: Int) {
            val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val service = TransferForegroundService()
            manager.notify(NOTIFICATION_ID, service.buildNotification(context, title, text, progress))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, TransferForegroundService::class.java))
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val title = intent?.getStringExtra(EXTRA_TITLE)?.takeIf { it.isNotBlank() } ?: "正在传输文件"
        val text = intent?.getStringExtra(EXTRA_TEXT)?.takeIf { it.isNotBlank() } ?: "保持后台运行"
        val progress = intent?.getIntExtra(EXTRA_PROGRESS, -1) ?: -1
        ensureChannel()
        startForeground(NOTIFICATION_ID, buildNotification(this, title, text, progress))
        return START_NOT_STICKY
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (manager.getNotificationChannel(CHANNEL_ID) != null) return
        val channel = NotificationChannel(
            CHANNEL_ID,
            "文件传输",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "上传/下载进行中的常驻通知"
            setShowBadge(false)
        }
        manager.createNotificationChannel(channel)
    }

    fun buildNotification(
        context: Context,
        title: String,
        text: String,
        progress: Int,
    ): Notification {
        val intent = Intent(context, MainActivity::class.java)
        val flags = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        } else {
            PendingIntent.FLAG_UPDATE_CURRENT
        }
        val pending = PendingIntent.getActivity(context, 0, intent, flags)
        val builder = NotificationCompat.Builder(context, CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setContentIntent(pending)
        if (progress >= 0) {
            builder.setProgress(100, progress.coerceIn(0, 100), false)
        } else {
            builder.setProgress(0, 0, true)
        }
        return builder.build()
    }
}
