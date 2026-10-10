package com.motolink.feature.tracking

import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.motolink.MotoLinkApp
import com.motolink.core.db.LocalSessionEntity
import com.motolink.core.location.LocationPermissions
import com.motolink.core.location.LocationSource
import com.motolink.core.location.LocationSources
import com.motolink.logic.AdaptiveSampler
import com.motolink.logic.Fix
import com.motolink.logic.FixDecision
import com.motolink.logic.QualityFilter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import com.motolink.feature.sync.SyncWorker

/**
 * Foreground service (type "location", research S2) that records GPS only while a session is
 * active. It is started by an explicit user action (ADR-013/ADR-016) and stops GPS on pause/end.
 *
 * Android may still kill it (docs/design/12 §5.3). State is in Room, so nothing already
 * recorded is lost; the UI detects an interruption as "open session but service not running".
 */
class TrackingService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val fixes = Channel<Fix>(capacity = 64, onBufferOverflow = kotlinx.coroutines.channels.BufferOverflow.DROP_OLDEST)
    private lateinit var source: LocationSource
    private val sampler = AdaptiveSampler()
    private val filter = QualityFilter()
    private var session: LocalSessionEntity? = null
    private var gpsOn = false

    private val container get() = (application as MotoLinkApp).container
    private val repo get() = container.tracking

    override fun onCreate() {
        super.onCreate()
        _running.value = true
        source = LocationSources.create(applicationContext)
        scope.launch { for (f in fixes) process(f) }
        // The server ended my session (trip ended, removed, timeout): stop recording into the void.
        scope.launch {
            container.realtime.trackingEnded.collect { sid ->
                if (sid == session?.sessionId) {
                    stopGps()
                    stopForegroundAndSelf()
                }
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // A null intent means the system restarted us after killing the process (START_STICKY).
        when (intent?.action) {
            ACTION_PAUSE -> scope.launch { changeState(pause = true) }
            ACTION_RESUME -> scope.launch { changeState(pause = false) }
            ACTION_END -> scope.launch { endSession() }
            else -> {
                // startForeground must follow startForegroundService within a few seconds, so do it
                // synchronously and only then look at the database (it stops itself if nothing is open).
                if (!enterForeground(paused = false)) {
                    stopSelf()
                    return START_NOT_STICKY
                }
                scope.launch { startOrRestore() }
            }
        }
        return START_STICKY
    }

    private suspend fun startOrRestore() {
        val perms = LocationPermissions.read(this)
        val open = repo.currentOpen()
        if (open == null || !perms.canRecord) {
            stopForegroundAndSelf()
            return
        }
        if (open.status == "paused") {
            getSystemService(android.app.NotificationManager::class.java)
                .notify(Notifications.ID_TRACKING, Notifications.tracking(this, paused = true))
        }
        session = open
        filter.reset()
        sampler.reset()
        if (open.status == "active") startGps() else stopGps()
    }

    private fun enterForeground(paused: Boolean): Boolean = try {
        val n = Notifications.tracking(this, paused)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ServiceCompat.startForeground(this, Notifications.ID_TRACKING, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        } else {
            startForeground(Notifications.ID_TRACKING, n)
        }
        true
    } catch (e: Exception) {
        // E.g. ForegroundServiceStartNotAllowedException when restarted from the background without the
        // required permission/state. Session stays open in Room; the UI offers to resume (user-initiated).
        Log.w(TAG, "cannot enter foreground", e)
        false
    }

    private suspend fun changeState(pause: Boolean) {
        val s = session ?: repo.currentOpen() ?: return stopForegroundAndSelf()
        session = s
        if (pause) {
            repo.pause(s.sessionId)
            stopGps()
        } else {
            repo.resume(s.sessionId)
            startGps()
        }
        syncServer(s) // best effort; owed calls are retried by SyncWorker
        getSystemService(android.app.NotificationManager::class.java)
            .notify(Notifications.ID_TRACKING, Notifications.tracking(this, paused = pause))
    }

    private suspend fun endSession() {
        val s = session ?: repo.currentOpen()
        stopGps()
        if (s != null) {
            repo.end(s.sessionId)
            if (s.serverIssued) {
                // Unsent points are drained before the session closes on the server (it refuses them afterwards).
                val ok = withTimeoutOrNull(10_000) { container.sessionSync.push(s.sessionId) } ?: false
                if (!ok) SyncWorker.kick(applicationContext)
            }
        }
        stopForegroundAndSelf()
    }

    private fun syncServer(s: LocalSessionEntity) {
        if (!s.serverIssued) return
        container.appScope.launch {
            if (!container.sessionSync.push(s.sessionId)) SyncWorker.kick(applicationContext)
        }
    }

    private fun startGps() {
        if (gpsOn) return
        gpsOn = true
        source.start(
            sampler.current,
            object : LocationSource.Listener {
                override fun onFix(fix: Fix) {
                    fixes.trySend(fix)
                }

                override fun onAvailabilityChanged(available: Boolean) {
                    _gpsAvailable.value = available
                }
            },
        )
    }

    private fun stopGps() {
        if (!gpsOn) return
        gpsOn = false
        source.stop() // GPS off immediately on pause/end (docs/design/11 §4.1)
    }

    private suspend fun process(f: Fix) {
        val s = session ?: return
        when (val d = filter.evaluate(f)) {
            is FixDecision.Reject -> Log.d(TAG, "fix rejected: ${d.reason}") // reason only, never coordinates
            is FixDecision.Accept -> {
                repo.append(s.sessionId, f, d.suspect, s.serverIssued)
                sampler.onFix(f.clientTsMs, f.speedMps)?.let { source.update(it) }
            }
        }
    }

    private fun stopForegroundAndSelf() {
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        stopGps()
        fixes.close()
        scope.cancel()
        _running.value = false
        super.onDestroy()
    }

    companion object {
        private const val TAG = "TrackingService"
        const val ACTION_START = "app.motolink.action.START"
        const val ACTION_PAUSE = "app.motolink.action.PAUSE"
        const val ACTION_RESUME = "app.motolink.action.RESUME"
        const val ACTION_END = "app.motolink.action.END"

        private val _running = MutableStateFlow(false)
        /** Process-local: false after a process kill, which is exactly how an interruption is detected. */
        val running: StateFlow<Boolean> = _running.asStateFlow()

        private val _gpsAvailable = MutableStateFlow(true)
        val gpsAvailable: StateFlow<Boolean> = _gpsAvailable.asStateFlow()

        fun intent(ctx: Context, action: String) = Intent(ctx, TrackingService::class.java).setAction(action)

        fun pendingAction(ctx: Context, action: String): PendingIntent =
            PendingIntent.getService(ctx, action.hashCode(), intent(ctx, action), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)

        /** Must be called from a user-initiated context (button) so the foreground start is allowed. */
        fun start(ctx: Context) = ContextCompat.startForegroundService(ctx, intent(ctx, ACTION_START))
        fun pause(ctx: Context) = ctx.startService(intent(ctx, ACTION_PAUSE))
        fun resume(ctx: Context) = ctx.startService(intent(ctx, ACTION_RESUME))
        fun end(ctx: Context) = ctx.startService(intent(ctx, ACTION_END))
    }
}
