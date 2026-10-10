package com.motolink.core.net

import com.motolink.BuildConfig
import com.motolink.logic.JwtExpiry
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

/** Adds a fresh Idempotency-Key to mutating requests that do not carry one (docs/design/06 §1.3). */
class IdempotencyInterceptor : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val req = chain.request()
        if (req.method == "GET" || req.header("Idempotency-Key") != null) return chain.proceed(req)
        return chain.proceed(req.newBuilder().header("Idempotency-Key", java.util.UUID.randomUUID().toString()).build())
    }
}

/**
 * Exchanges the refresh token (single-flight). Refresh tokens rotate, so two concurrent refreshes would trigger the
 * server's reuse detection; every caller (HTTP authenticator, WebSocket) therefore goes through this lock.
 */
class TokenRefresher(
    private val tokens: TokenStore,
    private val refreshApi: RefreshApi,
    private val events: SessionEvents,
) {
    private val lock = Any()

    /**
     * @param staleAccess the access token the caller used and found rejected/expiring; if another thread already
     * replaced it, the current tokens are returned without another round trip.
     * @return fresh tokens, or null when refreshing is impossible (signed out, offline, or rejected).
     */
    fun refreshBlocking(staleAccess: String?): Tokens? = synchronized(lock) {
        val current = tokens.current() ?: return null
        if (staleAccess != null && staleAccess != current.access) return current
        try {
            val fresh = runBlocking { refreshApi.refresh(RefreshRequest(current.refresh)) }
            val t = Tokens(fresh.accessToken, fresh.refreshToken)
            runBlocking { tokens.save(t) }
            t
        } catch (e: retrofit2.HttpException) {
            if (e.code() == 401) {
                runBlocking { tokens.clear() }
                events.signalExpired()
            }
            null
        } catch (e: java.io.IOException) {
            null // offline: keep the session, callers retry later
        }
    }

    /** Returns tokens whose access token stays valid for at least [minValidMs], refreshing when needed. */
    suspend fun ensureFresh(minValidMs: Long = 120_000, nowMs: Long = System.currentTimeMillis()): Tokens? =
        kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
            val t = tokens.current() ?: return@withContext null
            val exp = JwtExpiry.expiresAtMs(t.access)
            if (exp != null && exp - nowMs > minValidMs) t else refreshBlocking(t.access) ?: t.takeIf { exp != null && exp > nowMs }
        }
}

/** On 401, refreshes once and retries the request. */
class TokenAuthenticator(
    private val tokens: TokenStore,
    private val refresher: TokenRefresher,
) : Authenticator {
    override fun authenticate(route: Route?, response: Response): Request? {
        val path = response.request.url.encodedPath
        if (path.endsWith("/auth/refresh") || path.endsWith("/auth/login") || path.endsWith("/auth/register")) return null
        if (responseCount(response) >= 2) return null
        val sent = response.request.header("Authorization")?.removePrefix("Bearer ")
        val t = refresher.refreshBlocking(sent) ?: return null
        return response.request.newBuilder().header("Authorization", "Bearer ${t.access}").build()
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

    val refresher = TokenRefresher(tokens, refreshApi, events)

    val client: OkHttpClient = base()
        .addInterceptor(AuthInterceptor(tokens))
        .addInterceptor(IdempotencyInterceptor())
        .authenticator(TokenAuthenticator(tokens, refresher))
        .build()

    val api: ApiService = retrofit(client).create(ApiService::class.java)
}
