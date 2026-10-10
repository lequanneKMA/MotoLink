package com.motolink.core.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/** Local view of one of my tracking sessions (docs/design/12 §3: local_session). */
@Entity(tableName = "local_session")
data class LocalSessionEntity(
    @PrimaryKey @ColumnInfo(name = "session_id") val sessionId: String,
    /** Server trip this session belongs to; null while recording solo (M1, no backend yet). */
    @ColumnInfo(name = "trip_id") val tripId: String?,
    /** active | paused | ended */
    val status: String,
    @ColumnInfo(name = "started_at") val startedAt: Long,
    @ColumnInfo(name = "ended_at") val endedAt: Long?,
    /** Last allocated seq. Allocation happens in the same transaction as the point insert (ADR-004). */
    @ColumnInfo(name = "last_seq") val lastSeq: Long,
    /** True when the server issued session_id; false for solo/local-only sessions that must not be uploaded. */
    @ColumnInfo(name = "server_issued") val serverIssued: Boolean,
)

/**
 * A recorded point. Doubles as the upload buffer (acked = false) and as the local history.
 * PK (session_id, seq) makes resends idempotent on both sides (ADR-004/005).
 */
@Entity(
    tableName = "track_point",
    primaryKeys = ["session_id", "seq"],
    indices = [Index(value = ["session_id", "acked"])],
)
data class TrackPointEntity(
    @ColumnInfo(name = "session_id") val sessionId: String,
    val seq: Long,
    @ColumnInfo(name = "client_ts") val clientTs: Long,
    val lat: Double,
    val lng: Double,
    @ColumnInfo(name = "accuracy_m") val accuracyM: Float?,
    @ColumnInfo(name = "speed_mps") val speedMps: Float?,
    @ColumnInfo(name = "heading_deg") val headingDeg: Float?,
    @ColumnInfo(name = "altitude_m") val altitudeM: Double?,
    val suspect: Boolean,
    /** Server confirmed durable storage (location.ack) or point is local-only history. */
    val acked: Boolean,
)
