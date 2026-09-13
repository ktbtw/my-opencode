package com.chatcodex.chat_codex_app

import android.app.Activity
import android.content.Intent
import android.database.Cursor
import android.net.Uri
import android.os.Bundle
import android.provider.OpenableColumns

class OverlayFilePickerActivity : Activity() {
    companion object {
        private const val REQUEST_FILE = 1842
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) {
            startActivityForResult(
                Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE)
                    type = "*/*"
                    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                },
                REQUEST_FILE,
            )
        }
    }

    @Deprecated("Activity result API is sufficient for this small transparent picker")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST_FILE && resultCode == RESULT_OK) {
            val uris = buildList {
                data?.clipData?.let { clip ->
                    for (index in 0 until clip.itemCount) add(clip.getItemAt(index).uri)
                }
                data?.data?.let { uri ->
                    if (none { it == uri }) add(uri)
                }
            }
            uris.forEach { uri ->
                runCatching {
                    contentResolver.takePersistableUriPermission(
                        uri,
                        Intent.FLAG_GRANT_READ_URI_PERMISSION,
                    )
                }
                sendSelection(uri)
            }
        }
        finish()
    }

    private fun sendSelection(uri: Uri) {
        val name = contentResolver.query(
            uri,
            arrayOf(OpenableColumns.DISPLAY_NAME),
            null,
            null,
            null,
        )?.use { cursor: Cursor ->
            if (cursor.moveToFirst()) cursor.getString(0) else null
        }.orEmpty().ifBlank { uri.lastPathSegment ?: "附件" }
        sendBroadcast(Intent(GlobalOverlayService.ACTION_FILE_SELECTED).apply {
            setPackage(packageName)
            putExtra("uri", uri.toString())
            putExtra("name", name)
        })
    }
}
