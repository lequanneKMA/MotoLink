package com.motolink.feature.notifications

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import com.motolink.MotoLinkApp
import com.motolink.feature.sync.SyncWorker
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

class MotoLinkMessagingService : FirebaseMessagingService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onNewToken(token: String) {
        val c = (application as MotoLinkApp).container
        scope.launch { c.push.onToken(token) }
    }

    /** Data-only handling for the foreground; background `notification` messages are shown by the system. */
    override fun onMessageReceived(message: RemoteMessage) {
        val n = message.notification
        AppNotifications.showPush(this, n?.title, n?.body, message.data)
        // Wake the app's own delivery paths: a SOS push means the socket probably missed it.
        (application as MotoLinkApp).container.realtime.acquire()
        SyncWorker.kick(this)
    }
}
