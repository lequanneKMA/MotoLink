package com.motolink.core.location

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat

/** Snapshot of what the app may do right now. */
data class LocationPermissions(
    val fine: Boolean,
    val coarse: Boolean,
    val background: Boolean,
    val notifications: Boolean,
) {
    /** Enough to start recording: any foreground location. */
    val canRecord: Boolean get() = fine || coarse
    /** Fine location was not granted although coarse was (user picked "approximate", Android 12+). */
    val approximateOnly: Boolean get() = coarse && !fine

    companion object {
        fun read(ctx: Context): LocationPermissions {
            fun granted(p: String) = ContextCompat.checkSelfPermission(ctx, p) == PackageManager.PERMISSION_GRANTED
            return LocationPermissions(
                fine = granted(Manifest.permission.ACCESS_FINE_LOCATION),
                coarse = granted(Manifest.permission.ACCESS_COARSE_LOCATION),
                background = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) granted(Manifest.permission.ACCESS_BACKGROUND_LOCATION) else true,
                notifications = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) granted(Manifest.permission.POST_NOTIFICATIONS) else true,
            )
        }
    }
}
