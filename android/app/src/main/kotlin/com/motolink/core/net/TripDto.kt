package com.motolink.core.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Shapes of docs/design/06 §2.3–2.8 as the server implements them. */
@Serializable
data class TripDto(
    val id: String,
    @SerialName("owner_id") val ownerId: String,
    val name: String,
    val status: String,
    @SerialName("history_visibility") val historyVisibility: String = "members",
    @SerialName("max_members") val maxMembers: Int = 15,
    @SerialName("head_event_seq") val headEventSeq: Long = 0,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("ended_at") val endedAt: String? = null,
    @SerialName("created_at") val createdAt: String? = null,
)

@Serializable
data class MembershipDto(
    @SerialName("trip_id") val tripId: String,
    @SerialName("user_id") val userId: String,
    val role: String,
    val status: String,
    @SerialName("sharing_state") val sharingState: String = "sharing",
    @SerialName("share_history") val shareHistory: Boolean = true,
    val color: String? = null,
)

@Serializable
data class MemberDto(
    @SerialName("user_id") val userId: String,
    @SerialName("display_name") val displayName: String,
    val role: String,
    val status: String,
    @SerialName("sharing_state") val sharingState: String = "sharing",
    val color: String? = null,
)

@Serializable data class CreateTripRequest(val name: String, @SerialName("history_visibility") val historyVisibility: String = "members")
@Serializable data class TripEnvelope(val trip: TripDto)
@Serializable data class TripDetailResponse(val trip: TripDto, @SerialName("my_membership") val myMembership: MembershipDto, @SerialName("member_count") val memberCount: Int = 0)
@Serializable data class TripListResponse(val items: List<TripDto> = emptyList())
@Serializable data class MemberListResponse(val items: List<MemberDto> = emptyList())
@Serializable data class JoinRequest(val code: String)
@Serializable data class JoinResponse(val trip: TripDto, @SerialName("my_membership") val myMembership: MembershipDto)
@Serializable data class InvitePreview(@SerialName("trip_name") val tripName: String, @SerialName("member_count") val memberCount: Int, @SerialName("owner_name") val ownerName: String, val valid: Boolean)
@Serializable data class InviteRequest(@SerialName("expires_in_s") val expiresInS: Int = 86_400, @SerialName("max_uses") val maxUses: Int = 15)
@Serializable data class InvitationDto(val id: String, val code: String = "", @SerialName("expires_at") val expiresAt: String? = null, @SerialName("max_uses") val maxUses: Int = 0)
@Serializable data class MemberPatch(@SerialName("sharing_state") val sharingState: String? = null, @SerialName("share_history") val shareHistory: Boolean? = null)
@Serializable data class MembershipEnvelope(val membership: MembershipDto)

@Serializable
data class OpenSessionDto(val id: String, val status: String, @SerialName("last_seq") val lastSeq: Long = 0)

@Serializable
data class BootstrapTrip(
    val trip: TripDto,
    @SerialName("my_membership") val myMembership: MembershipDto,
    @SerialName("head_event_seq") val headEventSeq: Long = 0,
    @SerialName("my_open_session") val myOpenSession: OpenSessionDto? = null,
)

@Serializable data class BootstrapResponse(val trips: List<BootstrapTrip> = emptyList(), @SerialName("server_time") val serverTime: Long = 0)

@Serializable data class StartSessionRequest(@SerialName("device_id") val deviceId: String? = null)

@Serializable
data class SessionDto(
    val id: String,
    @SerialName("trip_id") val tripId: String,
    val status: String,
    @SerialName("session_month") val sessionMonth: String = "",
    @SerialName("last_seq") val lastSeq: Long = 0,
)

@Serializable data class SessionEnvelope(val session: SessionDto)

@Serializable data class PointsRequest(val points: List<com.motolink.logic.WirePoint>)
@Serializable data class RejectedDto(val seq: Long, val reason: String)
@Serializable data class PointsResponse(@SerialName("up_to_seq") val upToSeq: Long, val rejected: List<RejectedDto> = emptyList())

@Serializable
data class SosCreateRequest(
    @SerialName("client_sos_id") val clientSosId: String,
    val kind: String,
    val message: String? = null,
    val lat: Double? = null,
    val lng: Double? = null,
    @SerialName("accuracy_m") val accuracyM: Float? = null,
    @SerialName("client_created_at") val clientCreatedAt: Long? = null,
)

@Serializable
data class SosCreateResponse(@SerialName("sos_id") val sosId: String, @SerialName("event_seq") val eventSeq: Long = 0, val duplicate: Boolean = false)

@Serializable data class SosReceiptRequest(val status: String)
@Serializable data class SosResolveRequest(val status: String)

@Serializable
data class NotificationDto(
    val id: String,
    @SerialName("trip_id") val tripId: String? = null,
    val type: String,
    @SerialName("ref_event_id") val refEventId: String? = null,
    val payload: kotlinx.serialization.json.JsonObject? = null,
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("read_at") val readAt: String? = null,
)

@Serializable data class NotificationListResponse(val items: List<NotificationDto> = emptyList(), @SerialName("next_cursor") val nextCursor: String? = null)
@Serializable data class ReadNotificationsRequest(val ids: List<String> = emptyList(), val all: Boolean = false)

@Serializable
data class PutDeviceRequest(
    val platform: String = "android",
    val model: String,
    @SerialName("os_version") val osVersion: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("has_gms") val hasGms: Boolean,
    @SerialName("fcm_token") val fcmToken: String = "",
)

@Serializable data class ConsentPut(val version: String, val granted: Boolean)

/** Only the cursor of GET /trips/{id}/snapshot is needed by the resync path; the join frame carries the rest. */
@Serializable
data class SnapshotDto(@SerialName("head_event_seq") val headEventSeq: Long = 0)
