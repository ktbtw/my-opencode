package com.chatcodex.chat_codex_app

import android.content.Context
import android.util.Log
import cn.jpush.android.api.CmdMessage
import cn.jpush.android.api.JPushMessage

class JPushMessageReceiver : cn.jpush.android.service.JPushMessageReceiver() {
    override fun onRegister(context: Context, registrationId: String) {
        JPushBridge.saveRegistrationId(context, registrationId)
        Log.i(TAG, "onRegister registrationId=$registrationId")
    }

    override fun onConnected(context: Context, isConnected: Boolean) {
        super.onConnected(context, isConnected)
        Log.i(TAG, "onConnected isConnected=$isConnected")
    }

    override fun onCommandResult(context: Context, cmdMessage: CmdMessage) {
        super.onCommandResult(context, cmdMessage)
        Log.i(TAG, "onCommandResult errorCode=${cmdMessage.errorCode}")
    }

    override fun onAliasOperatorResult(context: Context, jPushMessage: JPushMessage) {
        super.onAliasOperatorResult(context, jPushMessage)
    }

    override fun onNotifyMessageOpened(context: Context, message: cn.jpush.android.api.NotificationMessage) {
        Log.i(
            TAG,
            "onNotifyMessageOpened notificationId=${message.notificationId} extras=${message.notificationExtras}",
        )
    }

    override fun onNotifyMessageArrived(
        context: Context,
        message: cn.jpush.android.api.NotificationMessage,
    ) {
        super.onNotifyMessageArrived(context, message)
    }

    companion object {
        private const val TAG = "JPushMessageReceiver"
    }
}
