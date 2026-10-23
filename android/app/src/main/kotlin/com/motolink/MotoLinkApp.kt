package com.motolink

import android.app.Application
import com.motolink.feature.notifications.AppNotifications
import com.motolink.feature.sync.SyncWorker
import com.motolink.feature.tracking.Notifications
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

class MotoLinkApp : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        Notifications.createChannels(this)
        AppNotifications.createChannels(this)
        container.realtime.start()
        container.appScope.launch { container.mapConfig.refresh() }
        SyncWorker.schedulePeriodic(this)
        // A SOS from another member must reach the user even when the app is in the background.
        container.appScope.launch { container.realtime.sosIncoming.collect { AppNotifications.showSos(this@MotoLinkApp, it) } }
        // Signed in (at start or after login): register the device/token and pull the server's view of my trips.
        container.appScope.launch {
            container.auth.signedIn.distinctUntilChanged().collect { signedIn ->
                if (signedIn) {
                    container.mapConfig.refresh()
                    container.push.register()
                    container.trips.refresh()
                    SyncWorker.kick(this@MotoLinkApp)
                }
            }
        }
        // A signed-out user must not keep a socket or cached trips of the previous account.
        container.appScope.launch {
            container.sessionEvents.expired.collect { container.realtime.reset() }
        }
    }
}
