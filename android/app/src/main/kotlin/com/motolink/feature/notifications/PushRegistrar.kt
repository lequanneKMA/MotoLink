package com.motolink.feature.notifications

import android.content.Context
import android.util.Log
import com.google.firebase.messaging.FirebaseMessaging
import com.motolink.BuildConfig
import com.motolink.core.net.ApiResult
import com.motolink.feature.trips.TripRepository
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume

/**
 * Registers this install and its FCM token with the server (PUT /devices/{id}). Without google-services.json the
 * device is still registered (model/version), just without a token, and push stays off.
 */
class PushRegistrar(private val context: Context, private val trips: TripRepository) {

    suspend fun register() {
        val token = if (BuildConfig.HAS_FCM) fetchToken() else null
        onToken(token)
    }

    suspend fun onToken(token: String?) {
        when (val r = trips.registerDevice(token)) {
            is ApiResult.Ok -> Unit
            is ApiResult.Err -> Log.i("PushRegistrar", "device registration deferred: ${r.failure}")
        }
    }

    private suspend fun fetchToken(): String? = suspendCancellableCoroutine { cont ->
        runCatching {
            FirebaseMessaging.getInstance().token
                .addOnSuccessListener { if (cont.isActive) cont.resume(it) }
                .addOnFailureListener { if (cont.isActive) cont.resume(null) }
        }.onFailure { if (cont.isActive) cont.resume(null) }
    }
}
