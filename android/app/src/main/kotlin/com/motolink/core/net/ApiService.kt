package com.motolink.core.net

import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST

interface ApiService {
    @POST("v1/auth/register")
    suspend fun register(@Body body: RegisterRequest): TokenResponse

    @POST("v1/auth/login")
    suspend fun login(@Body body: LoginRequest): TokenResponse

    @POST("v1/auth/logout")
    suspend fun logout()

    @GET("v1/me")
    suspend fun me(): MeResponse
}

/** Refresh is a separate interface/client: it must never carry (or trigger) the auth interceptors. */
interface RefreshApi {
    @POST("v1/auth/refresh")
    suspend fun refresh(@Body body: RefreshRequest): TokenResponse
}
