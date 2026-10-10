package com.motolink.feature.trips

import android.content.Context
import android.os.Build
import com.motolink.BuildConfig
import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.db.TripEntity
import com.motolink.core.location.LocationSources
import com.motolink.core.net.ApiResult
import com.motolink.core.net.ApiService
import com.motolink.core.net.BootstrapTrip
import com.motolink.core.net.ConsentPut
import com.motolink.core.net.CreateTripRequest
import com.motolink.core.net.InvitationDto
import com.motolink.core.net.InvitePreview
import com.motolink.core.net.InviteRequest
import com.motolink.core.net.JoinRequest
import com.motolink.core.net.MemberDto
import com.motolink.core.net.MemberPatch
import com.motolink.core.net.MembershipDto
import com.motolink.core.net.PutDeviceRequest
import com.motolink.core.net.StartSessionRequest
import com.motolink.core.net.SessionDto
import com.motolink.core.net.TokenStore
import com.motolink.core.net.TripDto
import com.motolink.core.net.apiCall
import com.motolink.core.net.onOk
import com.motolink.feature.tracking.TrackingRepository
import kotlinx.coroutines.flow.Flow

/** Trips, membership and server-issued tracking sessions. The server is authoritative; Room is a cache. */
class TripRepository(
    private val context: Context,
    private val api: ApiService,
    private val db: AppDatabase,
    private val tracking: TrackingRepository,
    private val tokens: TokenStore,
    private val clock: Clock = Clock.System,
) {
    val trips: Flow<List<TripEntity>> = db.trips().observeAll()
    fun trip(id: String): Flow<TripEntity?> = db.trips().observe(id)
    suspend fun cached(id: String): TripEntity? = db.trips().get(id)

    private suspend fun store(t: TripDto, m: MembershipDto) {
        // The cursor is advanced by realtime events/replay only; REST never moves it (keeps resume exact).
        db.trips().upsertKeepingCursor(TripEntity(t.id, t.name, t.status, t.ownerId, m.role, m.sharingState, 0, clock.nowMs()))
    }

    /** Full refresh (GET /sync/bootstrap); returns the server's view of my open sessions for reconciliation. */
    suspend fun refresh(): ApiResult<List<BootstrapTrip>> = apiCall { api.bootstrap().trips }.onOk { list ->
        list.forEach { store(it.trip, it.myMembership) }
        db.trips().deleteNotIn(list.map { it.trip.id }.ifEmpty { listOf("") })
        reconcileOpenSession(list)
    }

    /**
     * A local server-issued session the server no longer considers open (trip ended, timeout, removed) is closed
     * locally so the UI and the foreground service do not keep recording into the void.
     */
    private suspend fun reconcileOpenSession(list: List<BootstrapTrip>) {
        val local = db.sessions().open() ?: return
        if (!local.serverIssued) return
        val serverOpen = list.mapNotNull { it.myOpenSession?.id }.toSet()
        if (local.sessionId !in serverOpen && local.serverStatus == local.status) {
            db.sessions().end(local.sessionId, clock.nowMs())
            db.sessions().setServerStatus(local.sessionId, "ended")
        }
    }

    suspend fun create(name: String): ApiResult<TripDto> = apiCall { api.createTrip(CreateTripRequest(name.trim())).trip }.onOk { t ->
        db.trips().upsertKeepingCursor(TripEntity(t.id, t.name, t.status, t.ownerId, "owner", "sharing", 0, clock.nowMs()))
    }

    suspend fun preview(code: String): ApiResult<InvitePreview> = apiCall { api.resolveInvite(JoinRequest(code.trim())) }

    suspend fun join(code: String): ApiResult<TripDto> = apiCall { api.join(JoinRequest(code.trim())) }.let { r ->
        when (r) {
            is ApiResult.Ok -> {
                store(r.value.trip, r.value.myMembership)
                ApiResult.Ok(r.value.trip)
            }
            is ApiResult.Err -> r
        }
    }

    suspend fun start(id: String) = changeStatus(id) { api.startTrip(id).trip }
    suspend fun end(id: String) = changeStatus(id) { api.endTrip(id).trip }
    suspend fun archive(id: String) = changeStatus(id) { api.archiveTrip(id).trip }

    private suspend fun changeStatus(id: String, call: suspend () -> TripDto): ApiResult<TripDto> =
        apiCall(call).onOk { t ->
            db.trips().get(id)?.let { db.trips().upsert(it.copy(status = t.status, updatedAt = clock.nowMs())) }
        }

    suspend fun members(id: String): ApiResult<List<MemberDto>> = apiCall { api.members(id).items }
    suspend fun invite(id: String): ApiResult<InvitationDto> = apiCall { api.invite(id, InviteRequest()) }
    suspend fun remove(id: String, userId: String): ApiResult<Unit> = apiCall { api.removeMember(id, userId) }

    suspend fun leave(id: String): ApiResult<Unit> = apiCall { api.leaveTrip(id) }.onOk { db.trips().delete(id) }

    suspend fun setSharing(id: String, on: Boolean): ApiResult<MembershipDto> =
        apiCall { api.patchMe(id, MemberPatch(sharingState = if (on) "sharing" else "paused")).membership }.onOk { m ->
            db.trips().get(id)?.let { db.trips().upsert(it.copy(sharingState = m.sharingState)) }
        }

    /** The trip screen was told (event) or discovered (REST) a new durable cursor. */
    suspend fun advanceCursor(id: String, seq: Long) = db.trips().advanceCursor(id, seq)

    // ---- tracking sessions ----

    /** Starts a server-issued session for [tripId] and records it locally; points then upload with their seq. */
    suspend fun startSession(tripId: String): ApiResult<SessionDto> {
        val device = tokens.deviceId()
        var r = apiCall { api.startSession(tripId, StartSessionRequest(device)).session }
        if (r is ApiResult.Err && (r.failure as? com.motolink.core.net.ApiFailure.Server)?.code == "session_already_open") {
            // Already recording on another device/process: continue that session instead of failing.
            val open = (refresh() as? ApiResult.Ok)?.value?.firstOrNull { it.trip.id == tripId }?.myOpenSession
            if (open != null) r = ApiResult.Ok(SessionDto(open.id, tripId, open.status, lastSeq = open.lastSeq))
        }
        if (r is ApiResult.Ok) tracking.startServerSession(r.value.id, tripId, r.value.lastSeq)
        return r
    }

    suspend fun pauseSession(sid: String) = apiCall { api.pauseSession(sid).session }
    suspend fun resumeSession(sid: String) = apiCall { api.resumeSession(sid).session }
    suspend fun endSession(sid: String) = apiCall { api.endSession(sid).session }

    // ---- device / consent ----

    suspend fun registerDevice(fcmToken: String?): ApiResult<Unit> = apiCall {
        api.putDevice(
            tokens.deviceId(),
            PutDeviceRequest(
                model = "${Build.MANUFACTURER} ${Build.MODEL}".take(100), osVersion = "android-${Build.VERSION.SDK_INT}",
                appVersion = BuildConfig.VERSION_NAME, hasGms = LocationSources.hasPlayServices(context), fcmToken = fcmToken.orEmpty(),
            ),
        )
    }

    suspend fun grantLocationConsent(version: String = LOCATION_CONSENT_VERSION): ApiResult<Unit> = apiCall { api.putConsent("location_processing", ConsentPut(version, true)) }
    suspend fun withdrawLocationConsent(): ApiResult<Unit> = apiCall { api.putConsent("location_processing", ConsentPut(LOCATION_CONSENT_VERSION, false)) }
    suspend fun grantBackgroundConsent(version: String = LOCATION_CONSENT_VERSION): ApiResult<Unit> = apiCall { api.putConsent("background_location", ConsentPut(version, true)) }

    private companion object {
        /** Version string of the disclosure text the user accepted (kept in step with LocalPrefs.LOCATION_CONSENT_VERSION). */
        const val LOCATION_CONSENT_VERSION = "1"
    }
}
