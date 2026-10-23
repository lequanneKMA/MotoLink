package com.motolink.core.net

import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path
import retrofit2.http.Query

/**
 * Mutating calls get an Idempotency-Key from [IdempotencyInterceptor]; retries of the same logical action
 * must reuse the key by passing it through the `Idempotency-Key` header explicitly.
 */
interface ApiService {
    @POST("v1/auth/register")
    suspend fun register(@Body body: RegisterRequest): TokenResponse

    @POST("v1/auth/login")
    suspend fun login(@Body body: LoginRequest): TokenResponse

    @POST("v1/auth/logout")
    suspend fun logout()

    @GET("v1/config")
    suspend fun config(): ConfigDto

    @POST("v1/route")
    suspend fun route(@Body body: RouteRequest): RouteResponse

    @GET("v1/geocode")
    suspend fun geocode(@Query("q") q: String, @Query("lat") lat: Double? = null, @Query("lng") lng: Double? = null): PlacesResponse

    @GET("v1/me")
    suspend fun me(): MeResponse

    @PUT("v1/me/consents/{kind}")
    suspend fun putConsent(@Path("kind") kind: String, @Body body: ConsentPut)

    @PUT("v1/devices/{id}")
    suspend fun putDevice(@Path("id") deviceId: String, @Body body: PutDeviceRequest)

    // ---- trips ----
    @GET("v1/sync/bootstrap")
    suspend fun bootstrap(): BootstrapResponse

    @POST("v1/trips")
    suspend fun createTrip(@Body body: CreateTripRequest): TripEnvelope

    @GET("v1/trips")
    suspend fun trips(@Query("status") status: String? = null): TripListResponse

    @GET("v1/trips/{id}")
    suspend fun trip(@Path("id") id: String): TripDetailResponse

    @GET("v1/trips/{id}/snapshot")
    suspend fun snapshot(@Path("id") id: String): SnapshotDto

    @POST("v1/trips/{id}/start")
    suspend fun startTrip(@Path("id") id: String): TripEnvelope

    @POST("v1/trips/{id}/end")
    suspend fun endTrip(@Path("id") id: String): TripEnvelope

    @POST("v1/trips/{id}/archive")
    suspend fun archiveTrip(@Path("id") id: String): TripEnvelope

    @GET("v1/trips/{id}/members")
    suspend fun members(@Path("id") id: String): MemberListResponse

    @PATCH("v1/trips/{id}/members/me")
    suspend fun patchMe(@Path("id") id: String, @Body body: MemberPatch): MembershipEnvelope

    @DELETE("v1/trips/{id}/members/me")
    suspend fun leaveTrip(@Path("id") id: String)

    @DELETE("v1/trips/{id}/members/{userId}")
    suspend fun removeMember(@Path("id") id: String, @Path("userId") userId: String)

    @POST("v1/trips/{id}/invitations")
    suspend fun invite(@Path("id") id: String, @Body body: InviteRequest): InvitationDto

    @POST("v1/invitations/resolve")
    suspend fun resolveInvite(@Body body: JoinRequest): InvitePreview

    @POST("v1/trip-join")
    suspend fun join(@Body body: JoinRequest): JoinResponse

    // ---- tracking sessions ----
    @POST("v1/trips/{id}/tracking-sessions")
    suspend fun startSession(@Path("id") tripId: String, @Body body: StartSessionRequest): SessionEnvelope

    @POST("v1/tracking-sessions/{sid}/pause")
    suspend fun pauseSession(@Path("sid") sid: String): SessionEnvelope

    @POST("v1/tracking-sessions/{sid}/resume")
    suspend fun resumeSession(@Path("sid") sid: String): SessionEnvelope

    @POST("v1/tracking-sessions/{sid}/end")
    suspend fun endSession(@Path("sid") sid: String): SessionEnvelope

    @POST("v1/tracking-sessions/{sid}/points")
    suspend fun postPoints(@Path("sid") sid: String, @Body body: PointsRequest): PointsResponse

    // ---- SOS ----
    @POST("v1/trips/{id}/sos")
    suspend fun createSos(@Path("id") tripId: String, @Body body: SosCreateRequest): SosCreateResponse

    @POST("v1/sos/{id}/receipt")
    suspend fun sosReceipt(@Path("id") sosId: String, @Body body: SosReceiptRequest)

    @POST("v1/sos/{id}/resolve")
    suspend fun resolveSos(@Path("id") sosId: String, @Body body: SosResolveRequest)

    // ---- notifications ----
    @GET("v1/notifications")
    suspend fun notifications(@Query("cursor") cursor: String? = null): NotificationListResponse

    @POST("v1/notifications/read")
    suspend fun readNotifications(@Body body: ReadNotificationsRequest)
}

/** Refresh is a separate interface/client: it must never carry (or trigger) the auth interceptors. */
interface RefreshApi {
    @POST("v1/auth/refresh")
    suspend fun refresh(@Body body: RefreshRequest): TokenResponse
}
