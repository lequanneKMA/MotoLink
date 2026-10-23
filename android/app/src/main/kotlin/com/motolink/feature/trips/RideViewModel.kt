package com.motolink.feature.trips

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.motolink.AppContainer
import com.motolink.core.db.LocalSessionEntity
import com.motolink.core.db.TrackPointEntity
import com.motolink.core.db.TripEntity
import com.motolink.core.location.OneShotLocation
import com.motolink.core.net.ApiResult
import com.motolink.core.net.userMessage
import com.motolink.feature.realtime.ConnState
import com.motolink.logic.JwtExpiry
import com.motolink.logic.TripLiveState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

data class RideUi(
    val trip: TripEntity? = null,
    val live: TripLiveState? = null,
    val connection: ConnState = ConnState.OFFLINE,
    val mySession: LocalSessionEntity? = null,
    val myPoint: TrackPointEntity? = null,
    val pendingSos: Int = 0,
    val clockOffsetMs: Long = 0,
)

/** State and actions of one trip while riding (and of the screens opened from it). */
@OptIn(ExperimentalCoroutinesApi::class)
class RideViewModel(private val tripId: String, private val c: AppContainer) : ViewModel() {
    private val message = MutableStateFlow<String?>(null)
    val messages: StateFlow<String?> = message
    private val busy = MutableStateFlow(false)
    val working: StateFlow<Boolean> = busy
    private val _invite = MutableStateFlow<String?>(null)
    val invite: StateFlow<String?> = _invite

    private val mySession: Flow<LocalSessionEntity?> = c.tracking.openSession
    private val myPoint: Flow<TrackPointEntity?> =
        mySession.flatMapLatest { s -> if (s?.tripId == tripId) c.tracking.lastPoint(s.sessionId) else flowOf(null) }

    val ui: StateFlow<RideUi> = combine(
        c.trips.trip(tripId), c.realtime.live(tripId), c.realtime.connection, mySession, myPoint, c.sos.pendingCount, c.realtime.clockOffsetMs,
    ) { v ->
        RideUi(
            trip = v[0] as TripEntity?, live = v[1] as TripLiveState?, connection = v[2] as ConnState,
            mySession = (v[3] as LocalSessionEntity?)?.takeIf { it.tripId == tripId },
            myPoint = v[4] as TrackPointEntity?, pendingSos = v[5] as Int, clockOffsetMs = v[6] as Long,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), RideUi())

    /** Outcome of an SOS raised for this trip (arrives over the socket or never, if offline). */
    val sosOutcomes: Flow<Boolean> = flow {
        c.realtime.sosOutcome.collect { if (it.tripId == tripId) emit(it.ok) }
    }

    val me: String? get() = c.tokens.current()?.access?.let { JwtExpiry.subject(it) }

    init {
        c.realtime.watch(tripId)
        viewModelScope.launch { c.trips.refresh() }
    }

    override fun onCleared() {
        c.realtime.unwatch(tripId)
    }

    fun messageShown() {
        message.value = null
    }

    private fun work(block: suspend () -> Unit) {
        if (busy.value) return
        busy.value = true
        viewModelScope.launch {
            try {
                block()
            } finally {
                busy.value = false
            }
        }
    }

    private fun <T> ApiResult<T>.report(): Boolean = when (this) {
        is ApiResult.Ok -> true
        is ApiResult.Err -> { message.value = failure.userMessage(); false }
    }

    fun startTrip() = work { c.trips.start(tripId).report() }
    fun endTrip() = work { c.trips.end(tripId).report() }
    fun leave(onLeft: () -> Unit) = work { if (c.trips.leave(tripId).report()) onLeft() }
    fun setSharing(on: Boolean) = work { c.trips.setSharing(tripId, on).report() }
    fun remove(userId: String) = work { c.trips.remove(tripId, userId).report() }

    fun createInvite() = work {
        val r = c.trips.invite(tripId)
        if (r.report()) _invite.value = (r as ApiResult.Ok).value.code
    }

    fun inviteShown() {
        _invite.value = null
    }

    /** Creates the server session; [onReady] starts the foreground service (user-initiated). */
    fun startRecording(onReady: () -> Unit) = work {
        // The server requires the processing consent before any session (docs/design/11 §5.1).
        if (!c.trips.grantLocationConsent().report()) return@work
        if (c.trips.startSession(tripId).report()) onReady()
    }

    private val _sosInFlight = MutableStateFlow(false)
    /** True from the tap until the SOS is stored in the outbox (after that, `pendingSos` tells whether it arrived). */
    val sosInFlight: StateFlow<Boolean> = _sosInFlight

    /**
     * One tap sends. The position comes from the recording that is already running when it is fresh (instant);
     * otherwise a fix is awaited for at most 3 s: the alert must never wait long for coordinates.
     */
    fun sendSos(ctx: Context, kind: String) {
        _sosInFlight.value = true
        viewModelScope.launch {
            try {
                val p = ui.value.myPoint
                val fresh = p != null && System.currentTimeMillis() - p.clientTs < 60_000
                val fix = if (fresh) null else OneShotLocation.get(ctx, timeoutMs = 3_000)
                val lat = if (fresh) p!!.lat else fix?.lat
                val lng = if (fresh) p!!.lng else fix?.lng
                val acc = if (fresh) p!!.accuracyM else fix?.accuracyM
                c.sos.raise(tripId, kind, null, lat, lng, acc)
            } finally {
                _sosInFlight.value = false
            }
        }
    }

    fun markSeen(sosId: String) {
        c.realtime.sendReceipt(tripId, sosId, "seen")
        viewModelScope.launch { c.sos.receipt(sosId, "seen") }
    }

    fun resolveSos(sosId: String) = work { c.sos.resolve(sosId, "resolved").report() }
}
