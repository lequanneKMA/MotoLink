package com.motolink

import android.content.Context
import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.net.LocalPrefs
import com.motolink.core.net.NetworkModule
import com.motolink.core.net.SessionEvents
import com.motolink.core.net.TokenStore
import com.motolink.feature.auth.AuthRepository
import com.motolink.feature.tracking.TrackingRepository
import kotlinx.coroutines.runBlocking

/** Manual dependency container (ADR-018: no DI framework yet; revisit at M2, decision D-13). */
class AppContainer(context: Context) {
    private val app = context.applicationContext

    val clock = Clock.System
    val db: AppDatabase = AppDatabase.create(app)
    val tokens = TokenStore(app)
    val prefs = LocalPrefs(app)
    val sessionEvents = SessionEvents()

    init {
        // OkHttp interceptors read tokens synchronously; warm the cache once. A tiny DataStore read.
        runBlocking { tokens.load() }
    }

    val network = NetworkModule(
        tokens = tokens,
        versionName = BuildConfig.VERSION_NAME,
        deviceIdProvider = { null }, // device id is attached on register/login bodies; header is optional
        events = sessionEvents,
    )

    val auth = AuthRepository(app, network.api, tokens)
    val tracking = TrackingRepository(db, clock)
}
