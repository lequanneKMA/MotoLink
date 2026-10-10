package com.motolink.core.net

import com.motolink.BuildConfig
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import okhttp3.Authenticator
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.Route
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import java.util.concurrent.TimeUnit

/** Emits when the server rejected our refresh token: the UI must go back to the login screen. */
class SessionEvents {
    private val _expired = MutableSharedFlow<Unit>(extraBufferCapacity = 1)
    val expired: SharedFlow<Unit> = _expired.asSharedFlow()
    fun signalExpired() {
        _expired.tryEmit(Unit)
    }
}

class AuthInterceptor(private val tokens: TokenStore) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val req = chain.request()
        val t = tokens.current() ?: return chain.proceed(req)
        return chain.proceed(req.newBuilder().header("Authorization", "Bearer ${t.access}").build())
    }
}

/** Adds the app headers every API call carries (docs/design/06 §1.2). */
class AppHeadersInterceptor(private val versionName: String, private val deviceId: () -> String?) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val b = chain.request().newBuilder().header("X-App-Version", versionName)
        deviceId()?.let { b.header("X-Device-Id", it) }
        return chain.proceed(b.build())
    }
}

/**
 * On 401, exchanges the refresh token once (single-flight) and retries. A rejected refresh
 * (invalid/reused) clears the session. Refresh tokens rotate, so two concurrent refreshes
 * would trigger the server's reuse detection; hence the lock.
 */
class TokenAuthenticator(
    private val tokens: TokenStore,
    private val refreshApi: RefreshApi,
    private val events: SessionEvents,
) : Authenticator {
    private val lock = Any()

    override fun authenticate(route: Route?, response: Response): Request? {
        if (response.request.url.encodedPath.endsWith("/auth/refresh") || response.request.url.encodedPath.endsWith("/auth/login")) return null
        if (responseCount(response) >= 2) return null
        synchronized(lock) {
            val current = tokens.current() ?: return null
            val sent = response.request.header("Authorization")?.removePrefix("Bearer ")
            // Another thread already refreshed while we waited for the lock: just retry with the new token.
            if (sent != null && sent != current.access) return response.request.newBuilder().header("Authorization", "Bearer ${current.access}").build()
            return try {
                val fresh = runBlocking { refreshApi.refresh(RefreshRequest(current.refresh)) }
                val t = Tokens(fresh.accessToken, fresh.refreshToken)
                runBlocking { tokens.save(t) }
                response.request.newBuilder().header("Authorization", "Bearer ${t.access}").build()
            } catch (e: retrofit2.HttpException) {
                if (e.code() == 401) {
                    runBlocking { tokens.clear() }
                    events.signalExpired()
                }
                null
            } catch (e: java.io.IOException) {
                null // offline: keep the session, the call fails with 401 and the user can retry
            }
        }
    }

    private fun responseCount(r: Response): Int {
        var n = 1
        var p = r.priorResponse
        while (p != null) {
            n++
            p = p.priorResponse
        }
        return n
    }
}

class NetworkModule(
    tokens: TokenStore,
    private val versionName: String,
    private val deviceIdProvider: () -> String?,
    val events: SessionEvents,
) {
    val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }
    private val contentType = "application/json".toMediaType()

    private fun base(): OkHttpClient.Builder = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(15, TimeUnit.SECONDS)
        .writeTimeout(15, TimeUnit.SECONDS)
        .addInterceptor(AppHeadersInterceptor(versionName, deviceIdProvider))
        .also {
            if (BuildConfig.DEBUG) {
                // BASIC only: never log bodies (they carry credentials and coordinates).
                it.addInterceptor(HttpLoggingInterceptor().setLevel(HttpLoggingInterceptor.Level.BASIC))
            }
        }

    private fun retrofit(client: OkHttpClient) = Retrofit.Builder()
        .baseUrl(BuildConfig.API_BASE_URL)
        .client(client)
        .addConverterFactory(json.asConverterFactory(contentType))
        .build()

    private val plainClient = base().build()
    val refreshApi: RefreshApi = retrofit(plainClient).create(RefreshApi::class.java)

    val client: OkHttpClient = base()
        .addInterceptor(AuthInterceptor(tokens))
        .authenticator(TokenAuthenticator(tokens, refreshApi, events))
        .build()

    val api: ApiService = retrofit(client).create(ApiService::class.java)
}
