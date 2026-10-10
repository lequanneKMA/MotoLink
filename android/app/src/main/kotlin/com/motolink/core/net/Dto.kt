package com.motolink.core.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Shapes of docs/design/06 §2.1–2.2. */
@Serializable
data class DeviceDto(
    @SerialName("device_id") val deviceId: String,
    val model: String,
    @SerialName("os_version") val osVersion: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("has_gms") val hasGms: Boolean,
)

@Serializable
data class ConsentDto(val kind: String, val version: String)

@Serializable
data class RegisterRequest(
    val email: String,
    val password: String,
    @SerialName("display_name") val displayName: String,
    val device: DeviceDto,
    val consents: List<ConsentDto>,
)

@Serializable
data class LoginRequest(val email: String, val password: String, val device: DeviceDto)

@Serializable
data class RefreshRequest(@SerialName("refresh_token") val refreshToken: String)

@Serializable
data class UserDto(
    val id: String,
    val email: String,
    @SerialName("display_name") val displayName: String,
    @SerialName("avatar_color") val avatarColor: String = "#2E7D32",
    val locale: String = "vi",
)

@Serializable
data class TokenResponse(
    val user: UserDto? = null,
    @SerialName("access_token") val accessToken: String,
    @SerialName("expires_in") val expiresIn: Int,
    @SerialName("refresh_token") val refreshToken: String,
)

@Serializable
data class ConsentView(val kind: String, val version: String)

@Serializable
data class MeResponse(val user: UserDto, val consents: List<ConsentView> = emptyList())

@Serializable
data class FieldErrorDto(val field: String, val code: String)

/** RFC 9457-style error body (docs/design/06 §1.4). */
@Serializable
data class ProblemDto(
    val code: String,
    val title: String? = null,
    val status: Int = 0,
    val detail: String? = null,
    @SerialName("request_id") val requestId: String? = null,
    val errors: List<FieldErrorDto> = emptyList(),
)
