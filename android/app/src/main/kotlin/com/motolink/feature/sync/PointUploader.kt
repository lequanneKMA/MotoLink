package com.motolink.feature.sync

import com.motolink.core.db.AppDatabase
import com.motolink.core.db.TrackPointEntity
import com.motolink.core.net.ApiFailure
import com.motolink.core.net.ApiResult
import com.motolink.core.net.ApiService
import com.motolink.core.net.PointsRequest
import com.motolink.core.net.apiCall
import com.motolink.logic.WirePoint

fun TrackPointEntity.toWire() = WirePoint(seq, clientTs, lat, lng, accuracyM, speedMps, headingDeg, altitudeM)

/**
 * REST path for GPS points (docs/design/06 §2.6): used when the WebSocket is not available or the backlog is large.
 * The server answers with the same cumulative ACK semantics as `location.ack`; acked rows stay in Room as history.
 */
class PointUploader(private val api: ApiService, private val db: AppDatabase) {

    /** Sends everything unacked of [sessionId] in batches of 500. @return true when nothing is left to send. */
    suspend fun drain(sessionId: String): Boolean {
        while (true) {
            val batch = db.points().pending(sessionId, 500)
            if (batch.isEmpty()) return true
            when (val r = apiCall { api.postPoints(sessionId, PointsRequest(batch.map { it.toWire() })) }) {
                is ApiResult.Ok -> {
                    // Rejected points are permanently refused by validation; they are covered by up_to_seq as well.
                    db.points().ackUpTo(sessionId, r.value.upToSeq)
                }
                is ApiResult.Err -> {
                    val f = r.failure
                    // 404/409: the session does not exist or is closed on the server; retrying cannot help.
                    if (f is ApiFailure.Server && (f.status == 404 || f.code == "session_not_open")) {
                        db.points().ackUpTo(sessionId, Long.MAX_VALUE)
                        return true
                    }
                    return false
                }
            }
        }
    }
}
