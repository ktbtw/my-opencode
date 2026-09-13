package com.chatcodex.chat_codex_app

import android.content.Context

object JPushBridge {
    private const val prefsName = "jpush_bridge"
    private const val registrationIdKey = "registration_id"

    fun saveRegistrationId(context: Context, id: String) {
        if (id.isBlank()) {
            return
        }

        context.getSharedPreferences(prefsName, Context.MODE_PRIVATE)
            .edit()
            .putString(registrationIdKey, id)
            .apply()
    }

    fun getRegistrationId(context: Context): String {
        return context.getSharedPreferences(prefsName, Context.MODE_PRIVATE)
            .getString(registrationIdKey, "")
            .orEmpty()
    }
}
