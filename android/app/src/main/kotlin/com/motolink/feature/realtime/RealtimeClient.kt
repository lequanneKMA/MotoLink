package com.motolink.feature.realtime

import android.os.Build
import android.util.Log
import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.db.PendingSosEntity
import com.motolink.core.net.ApiService
import com.motolink.core.net.TokenRefresher
import com.motolink.core.net.TokenStore
import com.motolink.core.net.apiCall
import com.motolink.feature.sync.PointUploader
import com.motolink.feature.sync.toWire
import com.motolink.logic.Backoff
import com.motolink.logic.EventOutcome
import com.motolink.logic.HelloPayload
import com.motolink.logic.Inbound
import com.motolink.logic.JwtExpiry
import com.motolink.logic.LocationUpdatePayload
import com.motolink.logic.PendingTracker
import com.motolink.logic.ProtocolCodec
import com.motolink.logic.ResumeEntry
import com.motolink.logic.SosCreatePayload
import com.motolink.logic.SosEventPayload
import com.motolink.logic.TripLiveModel
import com.motolink.logic.TripLiveState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.util.UUID
import java.util.concurrent.TimeUnit

enum class ConnState { OFFLINE, CONNECTING, ONLINE, SUPERSEDED, UPGRADE_REQUIRED }

/** A SOS raised by someone else (the UI shows an alert; the notification layer raises a high-priority one). */
data class SosIncoming(val tripId: String, val eventSeq: Long, val sos: SosEventPayload)

/** Outcome of a queued SOS, so the UI can tell the user it arrived or why it never will. */
data class SosOutcome(val clientSosId: String, val tripId: String, val ok: Boolean, val code: String? = null)

private sealed interface Wire {
    data class Opened(val ws: WebSocket) : Wire
    data class Text(val ws: WebSocket, val text: String) : Wire
    data class Closed(val ws: WebSocket, val code: Int, val reason: String) : Wire
    data class Failed(val ws: WebSocket?, val error: Throwable, val httpCode: Int?) : Wire
}

/**
 * The single WebSocket of the app (ADR-009: one connection per user). It is held open while somebody needs it
 * (a screen watching a trip, a server-issued recording, a queued SOS) and owns: reconnect with resume
 * (docs/design/07 §6), live trip models, the GPS upload with cumulative ACK (ADR-005) and SOS delivery.
 *
 * All state is touched from one coroutine ([loop]/[handle]); OkHttp threads only enqueue [Wire] messages.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class RealtimeClient(
    http: OkHttpClient,
    private val baseUrl: String,
    private val tokens: TokenStore,
    private val refresher: TokenRefresher,
    private val db: AppDatabase,
    private val api: ApiService,
    private val uploader: PointUploader,
    private val versionName: String,
    private val clock: Clock = Clock.System,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
) {
    private val client = http.newBuilder().readTimeout(0, TimeUnit.MILLISECONDS).pingInterval(25, TimeUnit.SECONDS).build()

    private val _state = MutableStateFlow(ConnState.OFFLINE)
    val connection: StateFlow<ConnState> = _state.asStateFlow()

    private val _clockOffset = MutableStateFlow(0L)
    /** serverNow - clientNow, from `welcome` (docs/design/07 §3.1). */
    val clockOffsetMs: StateFlow<Long> = _clockOffset.asStateFlow()

    private val _sosIncoming = MutableSharedFlow<SosIncoming>(extraBufferCapacity = 16)
    val sosIncoming: SharedFlow<SosIncoming> = _sosIncoming.asSharedFlow()

    private val _sosOutcome = MutableSharedFlow<SosOutcome>(extraBufferCapacity = 16)
    val sosOutcome: SharedFlow<SosOutcome> = _sosOutcome.asSharedFlow()

    private val _trackingEnded = MutableSharedFlow<String>(extraBufferCapacity = 4)
    /** Emits the id of MY tracking session when the server ended it (trip ended, removed, timeout, other device). */
    val trackingEnded: SharedFlow<String> = _trackingEnded.asSharedFlow()
    private var myUserId: String? = null

    private val models = HashMap<String, TripLiveModel>()
    private val _live = MutableStateFlow<Map<String, TripLiveState>>(emptyMap())

    fun live(tripId: String): Flow<TripLiveState?> = _live.map { it[tripId] }.distinctUntilChanged()

    // ---- leases: who needs the socket ----
    private val watched = LinkedHashSet<String>()
    private var leases = 0
    private val wake = Channel<Unit>(Channel.CONFLATED)
    private var loopJob: Job? = null

    private val inbound = Channel<Wire>(Channel.UNLIMITED)
    private var socket: WebSocket? = null
    private var maxPoints = 20
    private var tracker = PendingTracker()
    private var trackerSession: String? = null
    private var uploadBlockedUntil = 0L
    private var restDraining = false
    private val joined = HashSet<String>()
    private val resyncing = HashSet<String>()
    private val backoff = Backoff()
    private var connectedAt = 0L
    private var halted = false
    private var penaltyMs = 0L
    private val requested = HashSet<String>() // trips the server joins from hello.resume

    /** Starts the connection manager; idempotent. Call once at app start. */
    fun start() {
        if (loopJob?.isActive == true) return
        loopJob = scope.launch { loop() }
    }

    /** A screen or service needs live data for [tripId]; pair with [unwatch]. */
    fun watch(tripId: String) {
        scope.launch {
            if (watched.add(tripId)) {
                models.getOrPut(tripId) { TripLiveModel(tripId, db.trips().get(tripId)?.lastEventSeq ?: 0) }
                if (socket != null && _state.value == ConnState.ONLINE) sendJoin(tripId)
            }
            halted = false
            wake.trySend(Unit)
        }
    }

    fun unwatch(tripId: String) {
        scope.launch {
            if (watched.remove(tripId) && joined.remove(tripId)) socket?.send(ProtocolCodec.tripLeave(tripId, newId()))
            models.remove(tripId)
            publish()
        }
    }

    /** Keeps the socket open without watching a trip (recording upload, pending SOS). */
    fun acquire() {
        scope.launch {
            leases++
            halted = false
            wake.trySend(Unit)
        }
    }

    fun release() {
        scope.launch { if (leases > 0) leases-- }
    }

    /** The user logged out: drop everything. */
    fun reset() {
        scope.launch {
            watched.clear(); joined.clear(); models.clear(); leases = 0
            publish()
            socket?.close(1000, "logout")
        }
    }

    /** Called when new GPS points were stored, so the uploader can run without waiting for the next tick. */
    fun pointsAvailable() {
        wake.trySend(Unit)
    }

    /** Sends a queued SOS right away when the socket is up; otherwise it waits in Room (resent on reconnect / SyncWorker). */
    fun trySendSos(s: PendingSosEntity) {
        scope.launch {
            val ws = socket
            if (ws != null && _state.value == ConnState.ONLINE) sendSos(ws, s) else {
                halted = false // a queued SOS keeps the connection manager trying (see needed())
                wake.trySend(Unit)
            }
        }
    }

    fun sendReceipt(tripId: String, sosId: String, status: String) {
        scope.launch { socket?.send(ProtocolCodec.sosReceipt(tripId, sosId, status, newId())) }
    }

    // ---------------------------------------------------------------------------------------------

    private suspend fun needed(): Boolean = !halted && (watched.isNotEmpty() || leases > 0 || db.sessions().open()?.serverIssued == true || db.sos().all().isNotEmpty())

    private suspend fun loop() {
        while (scope.isActive) {
            if (!needed() || tokens.current() == null) {
                _state.value = if (_state.value == ConnState.SUPERSEDED || _state.value == ConnState.UPGRADE_REQUIRED) _state.value else ConnState.OFFLINE
                withTimeoutOrNull(5_000) { wake.receive() }
                continue
            }
            val stable = connectOnce()
            if (!scope.isActive) return
            if (stable) backoff.reset()
            if (halted) continue
            delay(backoff.nextDelayMs() + penaltyMs.also { penaltyMs = 0 })
        }
    }

    /** One connection life. @return true if it stayed up long enough to count as stable. */
    private suspend fun connectOnce(): Boolean {
        _state.value = ConnState.CONNECTING
        val t = refresher.ensureFresh(minValidMs = 60_000) ?: return false
        while (inbound.tryReceive().isSuccess) { /* drop leftovers of a previous socket */ }
        joined.clear(); resyncing.clear()
        tracker.onReconnect()
        val url = baseUrl.trimEnd('/').replaceFirst("http", "ws") + "/v1/ws"
        val ws = client.newWebSocket(
            Request.Builder().url(url).header("Authorization", "Bearer ${t.access}").build(),
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) { inbound.trySend(Wire.Opened(webSocket)) }
                override fun onMessage(webSocket: WebSocket, text: String) { inbound.trySend(Wire.Text(webSocket, text)) }
                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) { inbound.trySend(Wire.Closed(webSocket, code, reason)) }
                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { inbound.trySend(Wire.Failed(webSocket, t, response?.code)) }
            },
        )
        socket = ws
        val ticker = scope.launch {
            var lastAuthCheck = 0L
            while (isActive) {
                delay(1_000)
                if (_state.value == ConnState.ONLINE) {
                    uploadTick(ws)
                    val now = clock.nowMs()
                    if (now - lastAuthCheck > 30_000) {
                        lastAuthCheck = now
                        keepTokenFresh(ws)
                    }
                }
            }
        }
        val wakeJob = scope.launch { for (u in wake) if (_state.value == ConnState.ONLINE) socket?.let { uploadTick(it) } }
        var stable = false
        try {
            while (scope.isActive) {
                val m = inbound.receive()
                when (m) {
                    is Wire.Opened -> if (m.ws === ws) onOpen(ws)
                    is Wire.Text -> if (m.ws === ws) handle(ws, ProtocolCodec.decode(m.text))
                    is Wire.Closed -> if (m.ws === ws) { stable = onClosed(m.code, m.reason); return stable }
                    is Wire.Failed -> if (m.ws === ws || m.ws == null) {
                        Log.i(TAG, "socket failed: ${m.error.javaClass.simpleName} http=${m.httpCode}")
                        if (m.httpCode == 401) refresher.refreshBlocking(t.access)
                        return connectedAt != 0L && clock.nowMs() - connectedAt > 60_000
                    }
                }
                if (halted) return false
            }
            return false
        } finally {
            ticker.cancelAndJoin(); wakeJob.cancel()
            ws.cancel()
            socket = null
            if (_state.value != ConnState.SUPERSEDED && _state.value != ConnState.UPGRADE_REQUIRED) _state.value = ConnState.OFFLINE
            connectedAt = 0
        }
    }

    private suspend fun onOpen(ws: WebSocket) {
        val resume = watched.map { ResumeEntry(it, models[it]?.cursor?.lastSeq ?: 0) }.take(20)
        requested.clear(); requested += resume.map { it.tripId }
        ws.send(
            ProtocolCodec.hello(
                HelloPayload(
                    deviceId = tokens.deviceId(), appVersion = versionName, os = "android-${Build.VERSION.SDK_INT}",
                    clientTimeMs = clock.nowMs(), resume = resume,
                ),
                id = newId(),
            ),
        )
    }

    private fun onClosed(code: Int, reason: String): Boolean {
        Log.i(TAG, "socket closed $code $reason")
        val stable = connectedAt != 0L && clock.nowMs() - connectedAt > 60_000
        when (code) {
            4409 -> { _state.value = ConnState.SUPERSEDED; halted = true } // another connection of this user won
            1008 -> if (reason.contains("upgrade_required")) { _state.value = ConnState.UPGRADE_REQUIRED; halted = true }
            4401 -> scope.launch(Dispatchers.IO) { refresher.refreshBlocking(tokens.current()?.access) }
            4429 -> penaltyMs = 30_000
        }
        return stable
    }

    private suspend fun keepTokenFresh(ws: WebSocket) {
        val cur = tokens.current() ?: return
        val exp = JwtExpiry.expiresAtMs(cur.access) ?: return
        if (exp - clock.nowMs() > 5 * 60_000) return
        val fresh = refresher.ensureFresh(minValidMs = 10 * 60_000) ?: return
        if (fresh.access != cur.access) ws.send(ProtocolCodec.authRefresh(fresh.access, newId()))
    }

    // ---- inbound ----

    private suspend fun handle(ws: WebSocket, m: Inbound) {
        when (m) {
            is Inbound.Welcome -> {
                connectedAt = clock.nowMs()
                myUserId = m.p.userId
                _clockOffset.value = m.p.clockOffsetMs
                maxPoints = m.p.limits.maxPointsPerFrame
                _state.value = ConnState.ONLINE
                // trips in `resume` are joined by the server; join the ones that appeared meanwhile
                for (t in watched) if (t !in requested && t !in joined) sendJoin(t)
                for (s in db.sos().all()) sendSos(ws, s)
                uploadTick(ws)
            }
            is Inbound.TripJoined -> {
                joined += m.tripId
                resyncing -= m.tripId
                val model = models[m.tripId] ?: return
                model.applySnapshot(m.p)
                publish()
            }
            is Inbound.LocationEvent -> {
                models[m.tripId]?.applyLocations(m.p.points)
                publish()
            }
            is Inbound.Presence -> {
                models[m.tripId]?.applyPresence(m.p)
                publish()
            }
            is Inbound.TripEvent -> {
                val model = models[m.tripId] ?: return
                val out = model.applyTripEvent(m.eventSeq, m.p)
                if (out == EventOutcome.Applied) noteOwnTrackingEnd(m.p)
                afterDurable(ws, m.tripId, model, out)
            }
            is Inbound.SosEvent -> {
                val model = models[m.tripId]
                val out = model?.applySos(m.eventSeq, m.p) ?: EventOutcome.Applied
                if (out == EventOutcome.Applied) {
                    _sosIncoming.tryEmit(SosIncoming(m.tripId, m.eventSeq, m.p))
                    ws.send(ProtocolCodec.sosReceipt(m.tripId, m.p.sosId, "received", newId()))
                }
                if (model != null) afterDurable(ws, m.tripId, model, out)
            }
            is Inbound.SosStatus -> {
                val tid = m.tripId
                if (tid != null) models[tid]?.applySosStatus(m.p)
                publish()
            }
            is Inbound.LocationAck -> onLocationAck(m.p.sessionId, m.p.upToSeq)
            is Inbound.Ack -> onAck(m.p.ref)
            is Inbound.Error -> onError(m.p)
            is Inbound.ResyncRequired -> m.tripId?.let { resync(ws, it) }
            is Inbound.Closing -> Log.i(TAG, "server closing: ${m.p.reason}")
            is Inbound.Pong, is Inbound.Unknown -> Unit
            is Inbound.Malformed -> Log.w(TAG, "malformed frame: ${m.reason}")
        }
    }

    private suspend fun noteOwnTrackingEnd(p: com.motolink.logic.TripEventPayload) {
        if (p.kind != "tracking_ended") return
        fun str(k: String) = (p.data?.get(k) as? kotlinx.serialization.json.JsonPrimitive)?.content
        val sid = str("session_id") ?: return
        if (str("user_id") != myUserId) return
        val local = db.sessions().get(sid) ?: return
        if (local.status == "ended") return
        db.sessions().end(sid, clock.nowMs())
        db.sessions().setServerStatus(sid, "ended") // the server already ended it
        _trackingEnded.tryEmit(sid)
    }

    private suspend fun afterDurable(ws: WebSocket, tripId: String, model: TripLiveModel, out: EventOutcome) {
        when (out) {
            EventOutcome.Applied -> {
                db.trips().advanceCursor(tripId, model.cursor.lastSeq)
                ws.send(ProtocolCodec.eventAck(tripId, model.cursor.lastSeq))
                publish()
            }
            EventOutcome.Duplicate -> Unit
            is EventOutcome.Gap -> resync(ws, tripId)
        }
    }

    /** Gap or `resync.required`: take the head from a REST snapshot, then join again from there (docs/design/07 §3.7). */
    private suspend fun resync(ws: WebSocket, tripId: String) {
        if (!resyncing.add(tripId)) return
        val head = (apiCall { api.snapshot(tripId) } as? com.motolink.core.net.ApiResult.Ok)?.value?.headEventSeq
        if (head == null) { resyncing.remove(tripId); return }
        models[tripId]?.cursor?.advanceTo(head)
        db.trips().advanceCursor(tripId, head)
        joined.remove(tripId)
        ws.send(ProtocolCodec.tripJoin(tripId, head, newId()))
    }

    private fun sendJoin(tripId: String) {
        val last = models[tripId]?.cursor?.lastSeq ?: 0
        socket?.send(ProtocolCodec.tripJoin(tripId, last, newId()))
    }

    private suspend fun onError(e: com.motolink.logic.ErrorPayload) {
        val ref = e.ref
        if (ref != null) {
            val sos = db.sos().all().firstOrNull { it.clientSosId == ref }
            if (sos != null) {
                if (!e.retryable) {
                    db.sos().delete(ref)
                    _sosOutcome.tryEmit(SosOutcome(ref, sos.tripId, ok = false, code = e.code))
                } else db.sos().bump(ref)
                return
            }
        }
        when (e.code) {
            "session_paused", "session_unknown" -> uploadBlockedUntil = clock.nowMs() + 5_000
            "server_busy", "rate_limited" -> uploadBlockedUntil = clock.nowMs() + (e.retryAfterMs ?: 1_500)
            "upgrade_required" -> { _state.value = ConnState.UPGRADE_REQUIRED; halted = true }
            else -> Log.w(TAG, "server error ${e.code} ref=$ref")
        }
    }

    private suspend fun onAck(ref: String) {
        val sos = db.sos().all().firstOrNull { it.clientSosId == ref } ?: return
        db.sos().delete(ref)
        _sosOutcome.tryEmit(SosOutcome(ref, sos.tripId, ok = true))
    }

    private suspend fun onLocationAck(sessionId: String, upTo: Long) {
        db.points().ackUpTo(sessionId, upTo)
        if (sessionId == trackerSession) tracker.onAck(upTo)
    }

    // ---- outbound: GPS ----

    private suspend fun uploadTick(ws: WebSocket) {
        val now = clock.nowMs()
        if (now < uploadBlockedUntil) return
        val s = db.sessions().open() ?: return
        val tripId = s.tripId
        if (!s.serverIssued || tripId == null || s.status != "active") return
        if (trackerSession != s.sessionId) {
            tracker = PendingTracker(); trackerSession = s.sessionId
        }
        val pending = db.points().pending(s.sessionId, 300)
        if (pending.isEmpty()) return
        if (pending.size > 200 && !restDraining) { // docs/design/07 §6 step 4: big backlogs go through REST
            restDraining = true
            scope.launch {
                try {
                    uploader.drain(s.sessionId)
                    tracker = PendingTracker()
                } finally {
                    restDraining = false
                }
            }
            return
        }
        if (restDraining) return
        pending.forEach { tracker.add(it.seq) }
        val bySeq = pending.associateBy { it.seq }
        val due = tracker.due(now, 100).mapNotNull { bySeq[it] }
        for (chunk in due.chunked(maxPoints)) {
            val ok = ws.send(ProtocolCodec.locationUpdate(tripId, LocationUpdatePayload(s.sessionId, chunk.map { it.toWire() }), now))
            if (!ok) return
            tracker.markSent(chunk.map { it.seq }, now)
        }
    }

    // ---- outbound: SOS ----

    private fun sendSos(ws: WebSocket, s: PendingSosEntity) {
        ws.send(
            ProtocolCodec.sosCreate(
                s.tripId,
                SosCreatePayload(s.clientSosId, s.kind, s.message, s.lat, s.lng, s.accuracyM, s.createdAt),
                clock.nowMs(),
            ),
        )
    }

    private fun publish() {
        _live.value = models.mapValues { it.value.state() }
    }

    private fun newId() = UUID.randomUUID().toString()

    private companion object {
        const val TAG = "RealtimeClient"
    }
}
