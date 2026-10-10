package com.motolink.feature.tracking

import com.motolink.core.common.Clock
import com.motolink.core.db.AppDatabase
import com.motolink.core.db.LocalSessionEntity
import com.motolink.core.db.TrackPointEntity
import com.motolink.logic.Fix
import com.motolink.logic.Geo
import kotlinx.coroutines.flow.Flow
import java.util.UUID

/**
 * Local source of truth for recording. M1 records solo sessions only; server-issued sessions
 * (ADR-004) plug in later through [startServerSession] without changing the point pipeline.
 */
class TrackingRepository(private val db: AppDatabase, private val clock: Clock = Clock.System) {

    val openSession: Flow<LocalSessionEntity?> = db.sessions().observeOpen()
    fun recentSessions(limit: Int = 50): Flow<List<LocalSessionEntity>> = db.sessions().observeRecent(limit)
    fun points(sessionId: String): Flow<List<TrackPointEntity>> = db.points().observe(sessionId)
    fun pointCount(sessionId: String): Flow<Int> = db.points().observeCount(sessionId)
    val pendingCount: Flow<Int> = db.points().observePendingCount()

    suspend fun currentOpen(): LocalSessionEntity? = db.sessions().open()

    /** Starts (or returns the existing) open solo session. */
    suspend fun startSolo(): LocalSessionEntity {
        db.sessions().open()?.let { return it }
        val s = LocalSessionEntity(
            sessionId = "local-" + UUID.randomUUID(), tripId = null, status = "active",
            startedAt = clock.nowMs(), endedAt = null, lastSeq = 0, serverIssued = false,
        )
        db.sessions().insert(s)
        return s
    }

    /** Records a session whose id the server issued (POST /trips/{id}/tracking-sessions). */
    suspend fun startServerSession(serverSessionId: String, tripId: String, lastSeq: Long = 0): LocalSessionEntity {
        // Only one open session at a time: a stale local row for the same server session is reused, others are closed.
        db.sessions().get(serverSessionId)?.let { existing ->
            if (existing.status == "ended") db.sessions().setStatus(serverSessionId, "active")
            return db.sessions().get(serverSessionId)!!
        }
        db.sessions().open()?.let { db.sessions().end(it.sessionId, clock.nowMs()) }
        val s = LocalSessionEntity(serverSessionId, tripId, "active", clock.nowMs(), null, lastSeq, true, serverStatus = "active")
        db.sessions().insert(s)
        return s
    }

    suspend fun pause(sessionId: String) = db.sessions().setStatus(sessionId, "paused")
    suspend fun resume(sessionId: String) = db.sessions().setStatus(sessionId, "active")
    suspend fun end(sessionId: String) = db.sessions().end(sessionId, clock.nowMs())

    /** Appends an accepted fix; seq is allocated atomically. Solo points have nothing to upload, so they are "acked". */
    suspend fun append(sessionId: String, fix: Fix, suspect: Boolean, serverIssued: Boolean): Long? =
        db.points().append(sessionId, fix, suspect, acked = !serverIssued)

    suspend fun delete(sessionId: String) {
        db.points().deleteForSession(sessionId)
        db.sessions().delete(sessionId)
    }

    /** Stats of a stored session, ignoring suspect points. */
    suspend fun stats(sessionId: String) = com.motolink.logic.RouteMath.stats(
        db.points().all(sessionId).filter { !it.suspect }.map { it.toFix() },
    )

    suspend fun purgeOldAcked(retentionMs: Long) = db.points().purgeAckedOlderThan(clock.nowMs() - retentionMs)
}

fun TrackPointEntity.toFix() = Fix(lat, lng, accuracyM, speedMps, headingDeg, altitudeM, clientTs)

@Suppress("unused")
private fun keep() = Geo // keeps the logic module import explicit for readers
