package com.motolink.core.location

import android.annotation.SuppressLint
import android.content.Context
import android.location.LocationManager
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.google.android.gms.tasks.CancellationTokenSource
import com.motolink.logic.Fix
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.resume

/**
 * A single position for SOS: a fresh fix if one arrives quickly, otherwise the newest known one.
 * Never blocks the SOS for long: the alert matters more than the coordinates (ADR-015).
 */
object OneShotLocation {
    @SuppressLint("MissingPermission")
    suspend fun get(ctx: Context, timeoutMs: Long = 6_000): Fix? {
        if (!LocationPermissions.read(ctx).canRecord) return null
        val fresh = if (LocationSources.hasPlayServices(ctx)) withTimeoutOrNull(timeoutMs) {
            suspendCancellableCoroutine { cont ->
                val cts = CancellationTokenSource()
                cont.invokeOnCancellation { cts.cancel() }
                LocationServices.getFusedLocationProviderClient(ctx)
                    .getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, cts.token)
                    .addOnSuccessListener { if (cont.isActive) cont.resume(it?.toFix()) }
                    .addOnFailureListener { if (cont.isActive) cont.resume(null) }
            }
        } else null
        if (fresh != null) return fresh
        return runCatching {
            val lm = ctx.getSystemService(LocationManager::class.java)
            lm.getProviders(true).mapNotNull { lm.getLastKnownLocation(it) }.maxByOrNull { it.time }?.toFix()
        }.getOrNull()
    }
}
