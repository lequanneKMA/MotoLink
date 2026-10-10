package com.motolink.feature.auth

import android.content.Context
import android.os.Build
import com.motolink.BuildConfig
import com.motolink.core.location.LocationSources
import com.motolink.core.net.ApiFailure
import com.motolink.core.net.ApiService
import com.motolink.core.net.ConsentDto
import com.motolink.core.net.DeviceDto
import com.motolink.core.net.LoginRequest
import com.motolink.core.net.RegisterRequest
import com.motolink.core.net.TokenResponse
import com.motolink.core.net.TokenStore
import com.motolink.core.net.Tokens
import com.motolink.core.net.UserDto
import com.motolink.core.net.toApiFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow

sealed interface AuthResult {
    data class Ok(val user: UserDto?) : AuthResult
    data class Failed(val failure: ApiFailure) : AuthResult
}

/** Version of the terms / privacy texts shown at registration (stored server-side as the consent version). */
private const val DOCS_VERSION = "2026-10-draft"

class AuthRepository(
    private val context: Context,
    private val api: ApiService,
    private val tokens: TokenStore,
) {
    val signedIn: Flow<Boolean> = tokens.signedIn

    private suspend fun device() = DeviceDto(
        deviceId = tokens.deviceId(),
        model = "${Build.MANUFACTURER} ${Build.MODEL}".take(100),
        osVersion = "android-${Build.VERSION.SDK_INT}",
        appVersion = BuildConfig.VERSION_NAME,
        hasGms = LocationSources.hasPlayServices(context),
    )

    suspend fun register(email: String, password: String, displayName: String): AuthResult = call {
        api.register(
            RegisterRequest(
                email = email.trim(), password = password, displayName = displayName.trim(), device = device(),
                consents = listOf(ConsentDto("terms", DOCS_VERSION), ConsentDto("privacy_policy", DOCS_VERSION)),
            ),
        )
    }

    suspend fun login(email: String, password: String): AuthResult =
        call { api.login(LoginRequest(email.trim(), password, device())) }

    /** Best effort: the local session is always cleared, even if the server cannot be reached. */
    suspend fun logout() {
        try {
            api.logout()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
        }
        tokens.clear()
    }

    private suspend fun call(block: suspend () -> TokenResponse): AuthResult = try {
        val r = block()
        tokens.save(Tokens(r.accessToken, r.refreshToken))
        AuthResult.Ok(r.user)
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        AuthResult.Failed(e.toApiFailure())
    }
}
