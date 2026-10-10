package com.motolink.core.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import com.motolink.logic.Fix
import kotlinx.coroutines.flow.Flow

@Dao
abstract class SessionDao {
    @Insert(onConflict = OnConflictStrategy.ABORT)
    abstract suspend fun insert(s: LocalSessionEntity)

    @Query("SELECT * FROM local_session WHERE session_id = :id")
    abstract suspend fun get(id: String): LocalSessionEntity?

    @Query("SELECT * FROM local_session WHERE session_id = :id")
    abstract fun observe(id: String): Flow<LocalSessionEntity?>

    /** The session that is still open (active or paused), if any. At most one by construction. */
    @Query("SELECT * FROM local_session WHERE status IN ('active','paused') ORDER BY started_at DESC LIMIT 1")
    abstract suspend fun open(): LocalSessionEntity?

    @Query("SELECT * FROM local_session WHERE status IN ('active','paused') ORDER BY started_at DESC LIMIT 1")
    abstract fun observeOpen(): Flow<LocalSessionEntity?>

    @Query("SELECT * FROM local_session ORDER BY started_at DESC LIMIT :limit")
    abstract fun observeRecent(limit: Int): Flow<List<LocalSessionEntity>>

    @Query("UPDATE local_session SET status = :status WHERE session_id = :id")
    abstract suspend fun setStatus(id: String, status: String)

    @Query("UPDATE local_session SET status = 'ended', ended_at = :endedAt WHERE session_id = :id")
    abstract suspend fun end(id: String, endedAt: Long)

    /** Marks sessions left open by a process kill as ended (used on cold start after an interruption). */
    @Query("UPDATE local_session SET status = 'ended', ended_at = :endedAt WHERE status IN ('active','paused')")
    abstract suspend fun endAllOpen(endedAt: Long): Int

    @Query("DELETE FROM local_session WHERE session_id = :id")
    abstract suspend fun delete(id: String)
}

@Dao
abstract class PointDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    abstract suspend fun insert(p: TrackPointEntity): Long

    @Query("SELECT last_seq FROM local_session WHERE session_id = :sessionId")
    abstract suspend fun lastSeq(sessionId: String): Long?

    @Query("UPDATE local_session SET last_seq = :seq WHERE session_id = :sessionId")
    abstract suspend fun setLastSeq(sessionId: String, seq: Long)

    /**
     * Allocates the next seq and stores the point atomically, so seq is never reused after
     * a process kill (docs/design/12 §4.2, ADR-004). [acked] is true for local-only sessions.
     * @return the seq assigned, or null if the session does not exist.
     */
    @Transaction
    open suspend fun append(sessionId: String, f: Fix, suspect: Boolean, acked: Boolean): Long? {
        val next = (lastSeq(sessionId) ?: return null) + 1
        insert(
            TrackPointEntity(
                sessionId = sessionId, seq = next, clientTs = f.clientTsMs, lat = f.lat, lng = f.lng,
                accuracyM = f.accuracyM, speedMps = f.speedMps, headingDeg = f.headingDeg, altitudeM = f.altitudeM,
                suspect = suspect, acked = acked,
            ),
        )
        setLastSeq(sessionId, next)
        return next
    }

    @Query("SELECT * FROM track_point WHERE session_id = :sessionId ORDER BY seq")
    abstract suspend fun all(sessionId: String): List<TrackPointEntity>

    @Query("SELECT * FROM track_point WHERE session_id = :sessionId ORDER BY seq")
    abstract fun observe(sessionId: String): Flow<List<TrackPointEntity>>

    @Query("SELECT COUNT(*) FROM track_point WHERE session_id = :sessionId")
    abstract fun observeCount(sessionId: String): Flow<Int>

    @Query("SELECT COUNT(*) FROM track_point WHERE acked = 0")
    abstract fun observePendingCount(): Flow<Int>

    @Query("SELECT * FROM track_point WHERE session_id = :sessionId AND acked = 0 ORDER BY seq LIMIT :limit")
    abstract suspend fun pending(sessionId: String, limit: Int): List<TrackPointEntity>

    /** location.ack: everything up to [upToSeq] is durable on the server (or permanently rejected). */
    @Query("UPDATE track_point SET acked = 1 WHERE session_id = :sessionId AND seq <= :upToSeq")
    abstract suspend fun ackUpTo(sessionId: String, upToSeq: Long): Int

    @Query("DELETE FROM track_point WHERE session_id = :sessionId")
    abstract suspend fun deleteForSession(sessionId: String)

    /** Retention of the local history: drop acked points of sessions that ended before [before]. */
    @Query(
        "DELETE FROM track_point WHERE acked = 1 AND session_id IN " +
            "(SELECT session_id FROM local_session WHERE ended_at IS NOT NULL AND ended_at < :before)",
    )
    abstract suspend fun purgeAckedOlderThan(before: Long): Int
}
