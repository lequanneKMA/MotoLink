package com.motolink.feature.sos

import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.db.PendingSosEntity
import com.motolink.core.net.ApiFailure
import com.motolink.core.net.ApiResult
import com.motolink.core.net.ApiService
import com.motolink.core.net.SosCreateRequest
import com.motolink.core.net.SosReceiptRequest
import com.motolink.core.net.SosResolveRequest
import com.motolink.core.net.apiCall
import com.motolink.feature.realtime.RealtimeClient
import kotlinx.coroutines.flow.Flow
import java.util.UUID

/**
 * SOS is stored locally first (so a tap in a dead zone is never lost) and delivered by whichever path works:
 * the WebSocket (resent on reconnect) or REST from the SyncWorker. The server dedupes on `client_sos_id`.
 */
class SosRepository(
    private val db: AppDatabase,
    private val api: ApiService,
    private val realtime: RealtimeClient,
    private val clock: Clock = Clock.System,
) {
    val pendingCount: Flow<Int> = db.sos().observeCount()

    suspend fun raise(tripId: String, kind: String, message: String?, lat: Double?, lng: Double?, accuracyM: Float?): String {
        val e = PendingSosEntity(UUID.randomUUID().toString(), tripId, kind, message?.trim()?.take(280)?.ifEmpty { null }, lat, lng, accuracyM, clock.nowMs(), 0)
        db.sos().insert(e)
        realtime.trySendSos(e)
        return e.clientSosId
    }

    /** REST delivery of queued SOS (SyncWorker, or when the socket is down). @return how many are still queued. */
    suspend fun flushViaRest(): Int {
        for (s in db.sos().all()) {
            val r = apiCall { api.createSos(s.tripId, SosCreateRequest(s.clientSosId, s.kind, s.message, s.lat, s.lng, s.accuracyM, s.createdAt)) }
            when (r) {
                is ApiResult.Ok -> db.sos().delete(s.clientSosId)
                is ApiResult.Err -> {
                    val f = r.failure
                    if (f is ApiFailure.Server && f.status in 400..499 && f.status != 408 && f.status != 429) db.sos().delete(s.clientSosId) // will never succeed
                    else db.sos().bump(s.clientSosId)
                }
            }
        }
        return db.sos().all().size
    }

    suspend fun receipt(sosId: String, status: String) = apiCall { api.sosReceipt(sosId, SosReceiptRequest(status)) }
    suspend fun resolve(sosId: String, status: String) = apiCall { api.resolveSos(sosId, SosResolveRequest(status)) }
}
