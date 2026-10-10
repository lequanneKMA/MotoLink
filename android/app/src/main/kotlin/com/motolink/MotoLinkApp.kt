package com.motolink

import android.app.Application
import com.motolink.feature.tracking.Notifications

class MotoLinkApp : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        Notifications.createChannels(this)
    }
}
