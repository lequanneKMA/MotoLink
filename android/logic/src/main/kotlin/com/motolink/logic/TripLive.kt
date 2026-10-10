package com.motolink.logic

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/** One trip member as the live screen shows it. */
data class MemberLive(
    val userId: String,
    val name: String,
    val color: String?,
    val role: String,
    val sharing: Boolean,
    val online: Boolean,
    val recording: Boolean,
    val location: LiveLocation?,
)

data class SosAlert(
    val sosId: String,
    val senderId: String,
    val senderName: String?,
    val kind: String,
    val message: String?,
    val lat: Double?,
    val lng: Double?,
    val createdAt: Long?,
    val eventSeq: Long,
    val status: String = "open",
    val delivery: SosStatusPayload? = null,
)

data class TripLiveState(
    val tripId: String,
    val name: String = "",
    val status: String = "",
    val headEventSeq: Long = 0,
    val members: List<MemberLive> = emptyList(),
    val sos: List<SosAlert> = emptyList(),
    val degraded: Boolean = false,
)

sealed interface EventOutcome {
    /** Applied (or kind unknown but cursor advanced). */
    data object Applied : EventOutcome
    data object Duplicate : EventOutcome
    /** Events [fromSeq]..[toSeq] are missing: the client must re-join with its cursor. */
    data class Gap(val fromSeq: Long, val toSeq: Long) : EventOutcome
}

/**
 * Client-side model of one joined trip (docs/design/07 §6). Pure and single-threaded: the caller serialises
 * access. Snapshots seed it; location events only move a marker forward (PositionKey ordering); durable events
 * are applied once and in order, a gap asks for a resync.
 */
class TripLiveModel(val tripId: String, lastEventSeq: Long = 0) {
    val cursor = EventCursor(lastEventSeq)
    private val locations = LocationReconciler<LiveLocation>()
    private val members = LinkedHashMap<String, MemberLive>()
    private val sosList = LinkedHashMap<String, SosAlert>()
    private var name = ""
    private var status = ""
    private var degraded = false

    fun applySnapshot(p: TripJoinedPayload) {
        name = p.trip.name
        status = p.trip.status
        degraded = p.degraded
        cursor.advanceTo(p.headEventSeq.coerceAtMost(p.replay.fromSeq - 1).coerceAtLeast(cursor.lastSeq))
        // Everything not covered by the replay is already in the snapshot.
        if (p.replay.count == 0 || !p.replay.complete) cursor.advanceTo(p.headEventSeq)
        val seen = HashSet<String>()
        for (m in p.members) {
            seen += m.userId
            val old = members[m.userId]
            val loc = m.location?.let { l ->
                val ll = LiveLocation(m.userId, l.sessionId, l.seq, l.lat, l.lng, l.accuracyM, l.speedMps, l.headingDeg, l.serverTs)
                locations.apply(m.userId, ll.key, ll)
                locations.get(m.userId)
            }
            members[m.userId] = MemberLive(
                userId = m.userId, name = m.displayName, color = m.color, role = m.role,
                sharing = m.sharingState == "sharing", online = m.presence == "online",
                recording = old?.recording ?: false,
                location = if (m.sharingState == "sharing") (loc ?: locations.get(m.userId)) else null,
            )
        }
        for (gone in members.keys.toList()) if (gone !in seen) dropMember(gone)
    }

    fun applyLocations(points: List<LiveLocation>) {
        for (p in points) {
            val m = members[p.userId] ?: continue
            if (!m.sharing) continue
            if (locations.apply(p.userId, p.key, p)) members[p.userId] = m.copy(location = p)
        }
    }

    fun applyPresence(p: PresencePayload) {
        val m = members[p.userId] ?: return
        members[p.userId] = m.copy(online = p.state == "online")
    }

    fun applyTripEvent(eventSeq: Long, p: TripEventPayload): EventOutcome = when (val d = cursor.offer(eventSeq)) {
        CursorDecision.Duplicate -> EventOutcome.Duplicate
        is CursorDecision.Gap -> EventOutcome.Gap(d.fromSeq, d.toSeq)
        CursorDecision.Apply -> {
            reduce(p)
            EventOutcome.Applied
        }
    }

    fun applySos(eventSeq: Long, p: SosEventPayload): EventOutcome = when (val d = cursor.offer(eventSeq)) {
        CursorDecision.Duplicate -> EventOutcome.Duplicate
        is CursorDecision.Gap -> EventOutcome.Gap(d.fromSeq, d.toSeq)
        CursorDecision.Apply -> {
            sosList[p.sosId] = SosAlert(p.sosId, p.senderId, p.senderName, p.kind, p.message, p.lat, p.lng, p.createdAt, eventSeq)
            EventOutcome.Applied
        }
    }

    fun applySosStatus(p: SosStatusPayload) {
        sosList[p.sosId]?.let { sosList[p.sosId] = it.copy(delivery = p) }
    }

    fun state(): TripLiveState = TripLiveState(
        tripId = tripId, name = name, status = status, headEventSeq = cursor.lastSeq,
        members = members.values.toList(), sos = sosList.values.toList().asReversed(), degraded = degraded,
    )

    private fun str(o: JsonObject?, k: String): String? = (o?.get(k) as? JsonPrimitive)?.contentOrNull

    private fun dropMember(userId: String) {
        members.remove(userId)
        locations.remove(userId)
    }

    private fun reduce(p: TripEventPayload) {
        val d = p.data
        when (p.kind) {
            "trip_started" -> status = "active"
            "trip_ended" -> status = "ended"
            "trip_archived" -> status = "archived"
            "member_joined" -> str(d, "user_id")?.let { id ->
                if (id !in members) members[id] = MemberLive(id, str(d, "display_name") ?: "?", null, "member", true, false, false, null)
            }
            "member_left", "member_removed" -> str(d, "user_id")?.let { dropMember(it) }
            "member_sharing_changed" -> str(d, "user_id")?.let { id ->
                members[id]?.let { m ->
                    val on = str(d, "sharing_state") == "sharing"
                    members[id] = m.copy(sharing = on, location = if (on) m.location else null)
                }
            }
            "ownership_transferred" -> {
                val from = str(d, "from")
                val to = str(d, "to")
                from?.let { f -> members[f]?.let { members[f] = it.copy(role = "member") } }
                to?.let { t -> members[t]?.let { members[t] = it.copy(role = "owner") } }
            }
            "tracking_started" -> setRecording(str(d, "user_id"), true)
            "tracking_ended" -> setRecording(str(d, "user_id"), false)
            "sos_resolved" -> str(d, "sos_id")?.let { id ->
                sosList[id]?.let { sosList[id] = it.copy(status = str(d, "status") ?: "resolved") }
            }
            else -> Unit // unknown kinds are ignored but the cursor has advanced
        }
    }

    private fun setRecording(userId: String?, on: Boolean) {
        val m = userId?.let { members[it] } ?: return
        members[m.userId] = m.copy(recording = on)
    }

    /** UI status of a member at [nowMs] (stale > 30 s, lost > 90 s since the last server timestamp). */
    fun statusOf(m: MemberLive, nowMs: Long, clockOffsetMs: Long): MemberStatus =
        PresenceClassifier.classify(m.online, m.sharing, m.location?.serverTs, nowMs, clockOffsetMs)
}
