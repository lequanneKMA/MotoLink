package com.motolink.logic

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement

/** WebSocket protocol v1, docs/design/07. Unknown fields and unknown types are tolerated (forward compatible). */
object Protocol {
    const val VERSION = 1

    val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true // `v` and `supported_v` are mandatory on the wire even though they equal their defaults
    }
}

@Serializable
data class Envelope(
    val v: Int = Protocol.VERSION,
    val type: String,
    val id: String? = null,
    @SerialName("trip_id") val tripId: String? = null,
    val ts: Long? = null,
    @SerialName("event_seq") val eventSeq: Long? = null,
    val payload: JsonElement? = null,
)

// ---- client -> server payloads ----
@Serializable
data class ResumeEntry(@SerialName("trip_id") val tripId: String, @SerialName("last_event_seq") val lastEventSeq: Long)

@Serializable
data class HelloPayload(
    @SerialName("device_id") val deviceId: String,
    @SerialName("app_version") val appVersion: String,
    val os: String,
    @SerialName("client_time_ms") val clientTimeMs: Long,
    @SerialName("supported_v") val supportedV: List<Int> = listOf(Protocol.VERSION),
    val resume: List<ResumeEntry> = emptyList(),
)

@Serializable
data class WirePoint(
    val seq: Long,
    @SerialName("client_ts") val clientTs: Long,
    val lat: Double,
    val lng: Double,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("speed_mps") val speedMps: Float? = null,
    @SerialName("heading_deg") val headingDeg: Float? = null,
    @SerialName("altitude_m") val altitudeM: Double? = null,
)

@Serializable
data class LocationUpdatePayload(@SerialName("session_id") val sessionId: String, val points: List<WirePoint>)

@Serializable
data class SosCreatePayload(
    @SerialName("client_sos_id") val clientSosId: String,
    val kind: String,
    val message: String? = null,
    val lat: Double? = null,
    val lng: Double? = null,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("client_created_at") val clientCreatedAt: Long? = null,
)

// ---- server -> client payloads ----
@Serializable
data class Heartbeat(
    @SerialName("server_ping_interval_s") val serverPingIntervalS: Int = 20,
    @SerialName("pong_timeout_s") val pongTimeoutS: Int = 10,
)

@Serializable
data class Limits(
    @SerialName("max_frame_bytes") val maxFrameBytes: Int = 16384,
    @SerialName("max_points_per_frame") val maxPointsPerFrame: Int = 20,
)

@Serializable
data class WelcomePayload(
    @SerialName("conn_id") val connId: String,
    @SerialName("user_id") val userId: String,
    @SerialName("server_time_ms") val serverTimeMs: Long,
    @SerialName("clock_offset_ms") val clockOffsetMs: Long = 0,
    @SerialName("v_min") val vMin: Int = 1,
    @SerialName("v_max") val vMax: Int = 1,
    val heartbeat: Heartbeat = Heartbeat(),
    val limits: Limits = Limits(),
)

@Serializable
data class MemberLocation(
    @SerialName("session_id") val sessionId: String,
    val seq: Long,
    val lat: Double,
    val lng: Double,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("speed_mps") val speedMps: Float? = null,
    @SerialName("heading_deg") val headingDeg: Float? = null,
    @SerialName("server_ts") val serverTs: Long,
) {
    val key: PositionKey get() = PositionKey(sessionId, seq)
}

@Serializable
data class SnapshotMember(
    @SerialName("user_id") val userId: String,
    @SerialName("display_name") val displayName: String,
    val color: String? = null,
    val role: String,
    @SerialName("sharing_state") val sharingState: String,
    val presence: String,
    val location: MemberLocation? = null,
)

@Serializable
data class ReplayInfo(@SerialName("from_seq") val fromSeq: Long = 0, val count: Int = 0, val complete: Boolean = true)

@Serializable
data class TripBrief(val status: String, val name: String)

@Serializable
data class TripJoinedPayload(
    val trip: TripBrief,
    @SerialName("head_event_seq") val headEventSeq: Long,
    val replay: ReplayInfo = ReplayInfo(),
    val members: List<SnapshotMember> = emptyList(),
    val degraded: Boolean = false,
)

@Serializable
data class LiveLocation(
    @SerialName("user_id") val userId: String,
    @SerialName("session_id") val sessionId: String,
    val seq: Long,
    val lat: Double,
    val lng: Double,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("speed_mps") val speedMps: Float? = null,
    @SerialName("heading_deg") val headingDeg: Float? = null,
    @SerialName("server_ts") val serverTs: Long,
) {
    val key: PositionKey get() = PositionKey(sessionId, seq)
}

@Serializable
data class LocationEventPayload(val points: List<LiveLocation>)

@Serializable
data class RejectedPoint(val seq: Long, val reason: String)

@Serializable
data class LocationAckPayload(
    @SerialName("session_id") val sessionId: String,
    @SerialName("up_to_seq") val upToSeq: Long,
    val rejected: List<RejectedPoint> = emptyList(),
)

@Serializable
data class PresencePayload(@SerialName("user_id") val userId: String, val state: String, val at: Long? = null)

@Serializable
data class TripEventPayload(
    @SerialName("event_id") val eventId: String,
    val kind: String,
    @SerialName("actor_id") val actorId: String? = null,
    val data: JsonObject? = null,
)

@Serializable
data class SosEventPayload(
    @SerialName("sos_id") val sosId: String,
    @SerialName("sender_id") val senderId: String,
    @SerialName("sender_name") val senderName: String? = null,
    val kind: String,
    val message: String? = null,
    val lat: Double? = null,
    val lng: Double? = null,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("created_at") val createdAt: Long? = null,
)

@Serializable
data class SosStatusPayload(
    @SerialName("sos_id") val sosId: String,
    val total: Int,
    val received: Int,
    val seen: Int,
    @SerialName("push_failed") val pushFailed: Int = 0,
)

@Serializable
data class AckPayload(val ref: String, val ok: Boolean = true, val result: JsonObject? = null)

@Serializable
data class ErrorPayload(
    val ref: String? = null,
    val code: String,
    val message: String? = null,
    val retryable: Boolean = false,
    @SerialName("retry_after_ms") val retryAfterMs: Long? = null,
)

@Serializable
data class ResyncPayload(val reason: String? = null)

@Serializable
data class ClosingPayload(val reason: String? = null, @SerialName("retry_after_ms") val retryAfterMs: Long? = null)

/** Parsed server message. Unknown types become [Unknown] and must be ignored by the caller. */
sealed interface Inbound {
    data class Welcome(val p: WelcomePayload) : Inbound
    data class TripJoined(val tripId: String, val p: TripJoinedPayload) : Inbound
    data class LocationEvent(val tripId: String, val p: LocationEventPayload) : Inbound
    data class LocationAck(val tripId: String?, val p: LocationAckPayload) : Inbound
    data class Presence(val tripId: String, val p: PresencePayload) : Inbound
    data class TripEvent(val tripId: String, val eventSeq: Long, val p: TripEventPayload) : Inbound
    data class SosEvent(val tripId: String, val eventSeq: Long, val p: SosEventPayload) : Inbound
    data class SosStatus(val tripId: String?, val p: SosStatusPayload) : Inbound
    data class Ack(val p: AckPayload) : Inbound
    data class Error(val p: ErrorPayload) : Inbound
    data class Pong(val ref: String?) : Inbound
    data class ResyncRequired(val tripId: String?, val p: ResyncPayload) : Inbound
    data class Closing(val p: ClosingPayload) : Inbound
    data class Unknown(val type: String) : Inbound
    /** Not JSON or missing mandatory envelope fields. */
    data class Malformed(val reason: String) : Inbound
}

object ProtocolCodec {
    private val json = Protocol.json

    // ---- encode ----
    private inline fun <reified T> frame(type: String, payload: T, id: String? = null, tripId: String? = null, ts: Long? = null): String =
        json.encodeToString(Envelope.serializer(), Envelope(type = type, id = id, tripId = tripId, ts = ts, payload = json.encodeToJsonElement(payload)))

    fun hello(p: HelloPayload, id: String? = null) = frame("hello", p, id = id, ts = p.clientTimeMs)
    fun authRefresh(accessToken: String, id: String) =
        frame("auth.refresh", JsonObject(mapOf("access_token" to kotlinx.serialization.json.JsonPrimitive(accessToken))), id = id)
    fun tripJoin(tripId: String, lastEventSeq: Long, id: String) =
        frame("trip.join", JsonObject(mapOf("last_event_seq" to kotlinx.serialization.json.JsonPrimitive(lastEventSeq))), id = id, tripId = tripId)
    fun tripLeave(tripId: String, id: String) = frame("trip.leave", JsonObject(emptyMap()), id = id, tripId = tripId)
    fun locationUpdate(tripId: String, p: LocationUpdatePayload, nowMs: Long) = frame("location.update", p, tripId = tripId, ts = nowMs)
    fun sosCreate(tripId: String, p: SosCreatePayload, nowMs: Long) = frame("sos.create", p, id = p.clientSosId, tripId = tripId, ts = nowMs)
    fun sosReceipt(tripId: String, sosId: String, status: String, id: String) = frame(
        "sos.receipt",
        JsonObject(mapOf("sos_id" to kotlinx.serialization.json.JsonPrimitive(sosId), "status" to kotlinx.serialization.json.JsonPrimitive(status))),
        id = id, tripId = tripId,
    )
    fun eventAck(tripId: String, eventSeq: Long) =
        frame("event.ack", JsonObject(mapOf("event_seq" to kotlinx.serialization.json.JsonPrimitive(eventSeq))), tripId = tripId)
    fun ping(id: String) = frame("ping", JsonObject(emptyMap()), id = id)

    // ---- decode ----
    fun decode(text: String): Inbound {
        val env = try {
            json.decodeFromString(Envelope.serializer(), text)
        } catch (e: Exception) {
            return Inbound.Malformed("invalid envelope: ${e.message?.take(80)}")
        }
        val p = env.payload
        return try {
            when (env.type) {
                "welcome" -> Inbound.Welcome(dec(p))
                "trip.joined" -> Inbound.TripJoined(need(env.tripId), dec(p))
                "location.event" -> Inbound.LocationEvent(need(env.tripId), dec(p))
                "location.ack" -> Inbound.LocationAck(env.tripId, dec(p))
                "presence.event" -> Inbound.Presence(need(env.tripId), dec(p))
                "trip.event" -> Inbound.TripEvent(need(env.tripId), needSeq(env.eventSeq), dec(p))
                "sos.event" -> Inbound.SosEvent(need(env.tripId), needSeq(env.eventSeq), dec(p))
                "sos.status" -> Inbound.SosStatus(env.tripId, dec(p))
                "ack" -> Inbound.Ack(dec(p))
                "error" -> Inbound.Error(dec(p))
                "pong" -> Inbound.Pong((p as? JsonObject)?.get("ref")?.let { (it as? kotlinx.serialization.json.JsonPrimitive)?.content })
                "resync.required" -> Inbound.ResyncRequired(env.tripId, dec(p))
                "closing" -> Inbound.Closing(dec(p))
                else -> Inbound.Unknown(env.type)
            }
        } catch (e: IllegalArgumentException) {
            Inbound.Malformed("${env.type}: ${e.message?.take(80)}")
        } catch (e: kotlinx.serialization.SerializationException) {
            Inbound.Malformed("${env.type}: ${e.message?.take(80)}")
        }
    }

    private inline fun <reified T> dec(p: JsonElement?): T {
        require(p != null) { "missing payload" }
        return json.decodeFromJsonElement(p)
    }

    private fun need(tripId: String?): String = requireNotNull(tripId) { "missing trip_id" }
    private fun needSeq(s: Long?): Long = requireNotNull(s) { "missing event_seq" }
}
