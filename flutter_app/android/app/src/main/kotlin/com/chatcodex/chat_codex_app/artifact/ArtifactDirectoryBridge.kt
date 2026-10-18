package com.chatcodex.chat_codex_app.artifact

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

/** SAF 目录树授权在进程重启和 Android 分区存储下依然有效。 */
class ArtifactDirectoryBridge(private val activity: Activity) {
    private var pending: MethodChannel.Result? = null
    private val io = Executors.newSingleThreadExecutor()
    private val requestCode = 4817

    fun handle(call: io.flutter.plugin.common.MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "pickDirectory" -> {
                if (pending != null) { result.error("busy", "目录选择器已打开", null); return }
                pending = result
                try {
                    activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
                        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or
                            Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_PREFIX_URI_PERMISSION)
                    }, requestCode)
                } catch (error: Exception) {
                    pending = null
                    result.error("pick_failed", error.message, null)
                }
            }
            "saveFilePath" -> {
                val directory = call.argument<String>("directory")
                val filename = call.argument<String>("filename")
                val sourcePath = call.argument<String>("sourcePath")
                val mime = call.argument<String>("mimeType") ?: "application/octet-stream"
                if (directory == null || filename.isNullOrBlank() || sourcePath.isNullOrBlank()) {
                    result.error("arguments", "文件信息不完整", null); return
                }
                io.execute {
                    var created: Uri? = null
                    try {
                        val source = java.io.File(sourcePath)
                        if (!source.exists()) throw IllegalStateException("源文件不存在")
                        val tree = Uri.parse(directory)
                        val parent = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
                        val uri = DocumentsContract.createDocument(activity.contentResolver, parent, mime, filename)
                            ?: throw IllegalStateException("创建文件失败，请重新选择目录")
                        created = uri
                        val stream = activity.contentResolver.openOutputStream(uri, "w")
                            ?: throw IllegalStateException("打开文件失败，请重新选择目录")
                        // 流式复制，避免把整个文件读进内存（大文件会 OOM）。
                        stream.use { output -> source.inputStream().use { input -> input.copyTo(output) } }
                        activity.runOnUiThread {
                            try {
                                activity.startActivity(Intent(Intent.ACTION_VIEW).apply {
                                    setDataAndType(uri, mime)
                                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                                })
                            } catch (_: Exception) { }
                            result.success(uri.toString())
                        }
                    } catch (error: Exception) {
                        try { created?.let { activity.contentResolver.delete(it, null, null) } } catch (_: Exception) {}
                        activity.runOnUiThread { result.error("save_failed", error.message, null) }
                    }
                }
            }
            "saveFile" -> {
                val directory = call.argument<String>("directory")
                val filename = call.argument<String>("filename")
                val bytes = call.argument<ByteArray>("bytes")
                val mime = call.argument<String>("mimeType") ?: "application/octet-stream"
                if (directory == null || filename.isNullOrBlank() || bytes == null) {
                    result.error("arguments", "文件信息不完整", null); return
                }
                io.execute {
                    var created: Uri? = null
                    try {
                        val tree = Uri.parse(directory)
                        val parent = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
                        val uri = DocumentsContract.createDocument(activity.contentResolver, parent, mime, filename)
                            ?: throw IllegalStateException("创建文件失败，请重新选择目录")
                        created = uri
                        val stream = activity.contentResolver.openOutputStream(uri, "w")
                            ?: throw IllegalStateException("打开文件失败，请重新选择目录")
                        stream.use { it.write(bytes) }
                        activity.runOnUiThread {
                            try {
                                activity.startActivity(Intent(Intent.ACTION_VIEW).apply {
                                    setDataAndType(uri, mime)
                                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                                })
                            } catch (_: Exception) { /* 即使没有可打开的查看器，文件也已保存成功。 */ }
                            result.success(uri.toString())
                        }
                    } catch (error: Exception) {
                        created?.let { try { DocumentsContract.deleteDocument(activity.contentResolver, it) } catch (_: Exception) {} }
                        activity.runOnUiThread { result.error("save_failed", "保存失败，请检查项目下载目录：${error.message}", null) }
                    }
                }
            }
            else -> result.notImplemented()
        }
    }

    fun onActivityResult(code: Int, status: Int, data: Intent?): Boolean {
        if (code != requestCode) return false
        val result = pending ?: return true
        pending = null
        val uri = data?.data
        if (status != Activity.RESULT_OK || uri == null) { result.success(null); return true }
        try {
            val flags = data.flags and (Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
            activity.contentResolver.takePersistableUriPermission(uri, flags)
            if (flags and Intent.FLAG_GRANT_WRITE_URI_PERMISSION == 0) throw IllegalStateException("请选择可写入的目录")
            result.success(mapOf("uri" to uri.toString(), "label" to DocumentsContract.getTreeDocumentId(uri)))
        } catch (error: Exception) {
            result.error("grant_failed", error.message, null)
        }
        return true
    }

    fun dispose() {
        pending?.error("closed", "目录选择已结束，请重试", null)
        pending = null
        io.shutdown()
    }
}
