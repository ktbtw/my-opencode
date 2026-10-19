package com.chatcodex.chat_codex_app.overlay

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.core.content.ContextCompat

/**
 * 麦克风授权结果回传总线。
 *
 * 悬浮层是 Service，没有 Activity 就弹不出运行时权限框，所以借一个透明页
 * [OverlayMicPermissionActivity] 去申请，结果再回到服务里继续启动语音球。
 */
internal object MicPermissionBus {
    var onResult: ((Boolean) -> Unit)? = null

    /** 只回调一次，避免透明页销毁时重复触发。 */
    fun deliver(granted: Boolean) {
        val callback = onResult
        onResult = null
        callback?.invoke(granted)
    }
}

/** 透明授权页：只为悬浮层的语音球申请 RECORD_AUDIO，拿到结果立刻结束。 */
class OverlayMicPermissionActivity : Activity() {
    private var delivered = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) ==
            PackageManager.PERMISSION_GRANTED
        ) {
            finishWith(true)
            finish()
            return
        }
        runCatching {
            requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), REQUEST_AUDIO)
        }.onFailure {
            finishWith(false)
            finish()
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        if (requestCode != REQUEST_AUDIO) {
            super.onRequestPermissionsResult(requestCode, permissions, grantResults)
            return
        }
        finishWith(grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED)
        finish()
    }

    override fun onDestroy() {
        super.onDestroy()
        // 用户直接返回、没走到授权回调时按拒绝处理，避免服务一直等结果。
        if (!delivered) finishWith(false)
    }

    private fun finishWith(granted: Boolean) {
        if (delivered) return
        delivered = true
        MicPermissionBus.deliver(granted)
    }


    companion object {
        private const val REQUEST_AUDIO = 7402

        fun intent(context: android.content.Context): Intent =
            Intent(context, OverlayMicPermissionActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
}
