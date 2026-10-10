package com.motolink.core.net

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import java.util.UUID

private val Context.dataStore by preferencesDataStore(name = "session")

data class Tokens(val access: String, val refresh: String)

/**
 * Persists tokens encrypted with [SecretBox], plus the stable per-install device id.
 * Reads are cached in memory so the OkHttp interceptor never blocks on disk.
 */
class TokenStore(context: Context, private val box: SecretBox = KeystoreSecretBox()) {
    private val ds = context.applicationContext.dataStore
    private val kAccess = stringPreferencesKey("access")
    private val kRefresh = stringPreferencesKey("refresh")
    private val kDevice = stringPreferencesKey("device_id")

    @Volatile private var cache: Tokens? = null
    @Volatile private var loaded = false

    /** Emits true when signed in. */
    val signedIn: Flow<Boolean> = ds.data.map { p ->
        p[kRefresh]?.let { box.open(it) } != null
    }

    suspend fun load(): Tokens? {
        if (loaded) return cache
        val p = ds.data.first()
        val a = p[kAccess]?.let { box.open(it) }
        val r = p[kRefresh]?.let { box.open(it) }
        cache = if (a != null && r != null) Tokens(a, r) else null
        loaded = true
        return cache
    }

    /** Non-suspending view for OkHttp threads; valid after [load] has run once at startup. */
    fun current(): Tokens? = cache

    suspend fun save(t: Tokens) {
        cache = t
        loaded = true
        ds.edit {
            it[kAccess] = box.seal(t.access)
            it[kRefresh] = box.seal(t.refresh)
        }
    }

    suspend fun clear() {
        cache = null
        loaded = true
        ds.edit {
            it.remove(kAccess)
            it.remove(kRefresh)
        }
    }

    suspend fun deviceId(): String {
        ds.data.first()[kDevice]?.let { return it }
        val id = UUID.randomUUID().toString()
        ds.edit { it[kDevice] = id }
        return id
    }
}
