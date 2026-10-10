package com.motolink.logic

/** Ordering key of a position (ADR-004): session ids are UUIDv7 so lexical order is time order. */
data class PositionKey(val sessionId: String, val seq: Long) : Comparable<PositionKey> {
    override fun compareTo(other: PositionKey): Int {
        val c = sessionId.compareTo(other.sessionId)
        return if (c != 0) c else seq.compareTo(other.seq)
    }

    fun isNewerThan(other: PositionKey?): Boolean = other == null || this > other
}

/**
 * Applies positions of other members only if newer than what is held, so a stale
 * snapshot or a delayed event can never move a marker backwards (docs/design/07 §6).
 */
class LocationReconciler<P> {
    private val held = HashMap<String, Pair<PositionKey, P>>()

    /** @return true if [point] replaced the held one. */
    fun apply(userId: String, key: PositionKey, point: P): Boolean {
        val cur = held[userId]
        if (!key.isNewerThan(cur?.first)) return false
        held[userId] = key to point
        return true
    }

    fun get(userId: String): P? = held[userId]?.second
    fun key(userId: String): PositionKey? = held[userId]?.first
    fun remove(userId: String) {
        held.remove(userId)
    }

    fun clear() = held.clear()
}

sealed interface CursorDecision {
    /** First time this event_seq is seen and it is contiguous: apply it. */
    data object Apply : CursorDecision
    data object Duplicate : CursorDecision
    /** A gap: events (lastSeq+1 until seq) are missing; fetch them or resync. */
    data class Gap(val fromSeq: Long, val toSeq: Long) : CursorDecision
}

/** Per-trip durable event cursor (docs/design/07 §3.5). Idempotent and gap-aware. */
class EventCursor(var lastSeq: Long = 0) {
    fun offer(seq: Long): CursorDecision = when {
        seq <= lastSeq -> CursorDecision.Duplicate
        seq == lastSeq + 1 -> {
            lastSeq = seq
            CursorDecision.Apply
        }
        else -> CursorDecision.Gap(lastSeq + 1, seq - 1)
    }

    /** After a REST catch-up or snapshot: jump forward (never backwards). */
    fun advanceTo(seq: Long) {
        if (seq > lastSeq) lastSeq = seq
    }
}

/**
 * Tracks GPS points awaiting server ACK (ADR-005). A point leaves the buffer only
 * when location.ack covers it; unacked points are resent after reconnect.
 */
class PendingTracker(private val resendAfterMs: Long = 5_000) {
    private val sentAt = java.util.TreeMap<Long, Long?>() // seq -> last send time (null = never sent)

    fun add(seq: Long) {
        sentAt.putIfAbsent(seq, null)
    }

    val size: Int get() = sentAt.size
    fun seqs(): List<Long> = sentAt.keys.toList()

    /** Seqs to (re)send now: never sent, or sent longer ago than [resendAfterMs]; ascending, at most [max]. */
    fun due(nowMs: Long, max: Int): List<Long> {
        val out = ArrayList<Long>(max)
        for ((seq, t) in sentAt) {
            if (out.size >= max) break
            if (t == null || nowMs - t >= resendAfterMs) out.add(seq)
        }
        return out
    }

    fun markSent(seqs: Collection<Long>, nowMs: Long) {
        for (s in seqs) if (sentAt.containsKey(s)) sentAt[s] = nowMs
    }

    /** On reconnect everything unacked is considered unsent. */
    fun onReconnect() {
        for (k in sentAt.keys.toList()) sentAt[k] = null
    }

    /**
     * Applies location.ack: removes every seq <= [upToSeq] (durable or permanently rejected)
     * and returns the removed seqs so the caller can delete them from Room.
     */
    fun onAck(upToSeq: Long): List<Long> {
        val head = sentAt.headMap(upToSeq, true)
        val removed = head.keys.toList()
        head.clear()
        return removed
    }
}

/** Exponential reconnect backoff 1s -> 30s with +/-50% jitter (docs/design/07 §4). */
class Backoff(
    private val baseMs: Long = 1_000,
    private val maxMs: Long = 30_000,
    private val random: () -> Double = Math::random,
) {
    private var attempt = 0

    fun nextDelayMs(): Long {
        val exp = minOf(maxMs, baseMs shl minOf(attempt, 20))
        attempt++
        val jitter = 0.5 + random() // 0.5 .. 1.5
        return (exp * jitter).toLong().coerceAtLeast(1)
    }

    fun reset() {
        attempt = 0
    }
}

enum class MemberStatus { SHARING_HIDDEN, OFFLINE, LIVE, STALE, LOST }

/** Derives the UI status of a member (docs/design/01 US-C2: stale >30s, lost >90s). */
object PresenceClassifier {
    fun classify(
        online: Boolean,
        sharing: Boolean,
        lastLocationServerTsMs: Long?,
        nowMs: Long,
        clockOffsetMs: Long = 0,
        staleAfterMs: Long = 30_000,
        lostAfterMs: Long = 90_000,
    ): MemberStatus {
        if (!sharing) return MemberStatus.SHARING_HIDDEN
        if (!online) return MemberStatus.OFFLINE
        val ts = lastLocationServerTsMs ?: return MemberStatus.LOST
        // clockOffsetMs = serverNow - clientNow, so server-time now = nowMs + offset
        val age = (nowMs + clockOffsetMs) - ts
        return when {
            age >= lostAfterMs -> MemberStatus.LOST
            age >= staleAfterMs -> MemberStatus.STALE
            else -> MemberStatus.LIVE
        }
    }
}

/** Local tracking session life cycle (docs/design/05 §5; the server is authoritative). */
enum class TrackState { IDLE, STARTING, ACTIVE, PAUSED, ENDED }

sealed interface TrackEvent {
    data object UserStart : TrackEvent
    data object ServerSessionCreated : TrackEvent
    data object StartFailed : TrackEvent
    data object UserPause : TrackEvent
    data object UserResume : TrackEvent
    data object UserEnd : TrackEvent
    /** Server ended it (trip ended, removed, timeout) or permission revoked. */
    data object ForcedEnd : TrackEvent
    data object Reset : TrackEvent
}

object TrackingMachine {
    /** @return next state, or null if the event is not allowed in [s]. */
    fun next(s: TrackState, e: TrackEvent): TrackState? = when (e) {
        TrackEvent.UserStart -> if (s == TrackState.IDLE || s == TrackState.ENDED) TrackState.STARTING else null
        TrackEvent.ServerSessionCreated -> if (s == TrackState.STARTING) TrackState.ACTIVE else null
        TrackEvent.StartFailed -> if (s == TrackState.STARTING) TrackState.IDLE else null
        TrackEvent.UserPause -> if (s == TrackState.ACTIVE) TrackState.PAUSED else null
        TrackEvent.UserResume -> if (s == TrackState.PAUSED) TrackState.ACTIVE else null
        TrackEvent.UserEnd -> if (s == TrackState.ACTIVE || s == TrackState.PAUSED) TrackState.ENDED else null
        TrackEvent.ForcedEnd -> if (s == TrackState.ACTIVE || s == TrackState.PAUSED || s == TrackState.STARTING) TrackState.ENDED else null
        TrackEvent.Reset -> TrackState.IDLE
    }

    /** GPS must be requested only in ACTIVE (stop on pause/end: docs/design/11 §4.1). */
    fun gpsAllowed(s: TrackState): Boolean = s == TrackState.ACTIVE
}
