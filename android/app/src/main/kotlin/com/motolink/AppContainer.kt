package com.motolink

import android.content.Context
import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.net.LocalPrefs
import com.motolink.core.net.NetworkModule
import com.motolink.core.net.SessionEvents
import com.motolink.core.net.TokenStore
import com.motolink.feature.auth.AuthRepository
import com.motolink.feature.map.MapConfigRepository
import com.motolink.feature.nav.NavRepository
import com.motolink.feature.notifications.PushRegistrar
import com.motolink.feature.realtime.RealtimeClient
import com.motolink.feature.sos.SosRepository
import com.motolink.feature.sync.PointUploader
import com.motolink.feature.sync.SessionSync
import com.motolink.feature.tracking.TrackingRepository
import com.motolink.feature.trips.TripRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.runBlocking

/** Manual dependency container (ADR-018: no DI framework yet; revisit at M2, decision D-13). */
class AppContainer(context: Context) {
    private val app = context.applicationContext

    val clock = Clock.System
    val appScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
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
    val trips = TripRepository(app, network.api, db, tracking, tokens, clock)

    val uploader = PointUploader(network.api, db)
    val sessionSync = SessionSync(network.api, db, uploader)
    val realtime = RealtimeClient(
        http = network.client, baseUrl = BuildConfig.API_BASE_URL, tokens = tokens, refresher = network.refresher,
        db = db, api = network.api, uploader = uploader, versionName = BuildConfig.VERSION_NAME, clock = clock,
    )
    val sos = SosRepository(db, network.api, realtime, clock)
    val push = PushRegistrar(app, trips)
    val mapConfig = MapConfigRepository(network.api)
    val nav = NavRepository(network.api)

    /** Logout: server call (best effort), then forget everything that belongs to the account. */
    suspend fun signOut() {
        auth.logout()
        realtime.reset()
        db.trips().clear()
        db.sos().clear()
    }
}
