package com.motolink.core.location

import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Looper
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.android.gms.location.LocationAvailability
import com.google.android.gms.location.LocationCallback
import com.google.android.gms.location.LocationRequest
import com.google.android.gms.location.LocationResult
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.motolink.logic.Fix
import com.motolink.logic.SamplingPlan

/**
 * Where GPS fixes come from (ADR-013): Fused Location Provider when Google Play Services is
 * present, plain LocationManager otherwise. Callers only see this interface.
 */
interface LocationSource {
    /** Begin delivering fixes using [plan]. Replaces any previous request. Requires location permission. */
    fun start(plan: SamplingPlan, listener: Listener)

    /** Change the request profile without losing the stream. */
    fun update(plan: SamplingPlan)

    fun stop()

    interface Listener {
        fun onFix(fix: Fix)

        /** GPS switched off / signal lost at system level. */
        fun onAvailabilityChanged(available: Boolean)
    }
}

object LocationSources {
    fun create(context: Context): LocationSource =
        if (hasPlayServices(context)) FusedLocationSource(context.applicationContext) else PlatformLocationSource(context.applicationContext)

    fun hasPlayServices(context: Context): Boolean =
        GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(context) == ConnectionResult.SUCCESS
}

internal fun Location.toFix(): Fix = Fix(
    lat = latitude,
    lng = longitude,
    accuracyM = if (hasAccuracy()) accuracy else null,
    speedMps = if (hasSpeed()) speed else null,
    headingDeg = if (hasBearing()) bearing else null,
    altitudeM = if (hasAltitude()) altitude else null,
    // Location.getTime() is the fix time reported by the provider; it is the device clock domain (ADR-004).
    clientTsMs = if (time > 0) time else System.currentTimeMillis(),
)

class FusedLocationSource(context: Context) : LocationSource {
    private val client = LocationServices.getFusedLocationProviderClient(context)
    private var listener: LocationSource.Listener? = null
    private var plan: SamplingPlan? = null

    private val callback = object : LocationCallback() {
        override fun onLocationResult(result: LocationResult) {
            val l = listener ?: return
            result.locations.forEach { l.onFix(it.toFix()) }
        }

        override fun onLocationAvailability(a: LocationAvailability) {
            listener?.onAvailabilityChanged(a.isLocationAvailable)
        }
    }

    @SuppressLint("MissingPermission") // permission is checked by TrackingService before start()
    override fun start(plan: SamplingPlan, listener: LocationSource.Listener) {
        this.listener = listener
        request(plan)
    }

    @SuppressLint("MissingPermission")
    override fun update(plan: SamplingPlan) {
        if (listener != null && plan != this.plan) request(plan)
    }

    @SuppressLint("MissingPermission")
    private fun request(plan: SamplingPlan) {
        this.plan = plan
        val req = LocationRequest.Builder(
            if (plan.highAccuracy) Priority.PRIORITY_HIGH_ACCURACY else Priority.PRIORITY_BALANCED_POWER_ACCURACY,
            plan.intervalMs,
        )
            .setMinUpdateIntervalMillis((plan.intervalMs / 2).coerceAtLeast(1_000))
            .setMinUpdateDistanceMeters(plan.minDistanceM)
            .setWaitForAccurateLocation(false)
            .build()
        // Re-requesting with the same callback replaces the previous request.
        client.requestLocationUpdates(req, callback, Looper.getMainLooper())
    }

    override fun stop() {
        client.removeLocationUpdates(callback)
        listener = null
        plan = null
    }
}

/** Fallback for devices without Google Play Services (no Fused provider). Lower quality, higher battery. */
class PlatformLocationSource(private val context: Context) : LocationSource {
    private val lm = context.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    private var listener: LocationSource.Listener? = null
    private var plan: SamplingPlan? = null

    private val ll = object : LocationListener {
        override fun onLocationChanged(location: Location) {
            listener?.onFix(location.toFix())
        }

        override fun onProviderEnabled(provider: String) {
            if (provider == LocationManager.GPS_PROVIDER) listener?.onAvailabilityChanged(true)
        }

        override fun onProviderDisabled(provider: String) {
            if (provider == LocationManager.GPS_PROVIDER) listener?.onAvailabilityChanged(false)
        }
    }

    @SuppressLint("MissingPermission")
    override fun start(plan: SamplingPlan, listener: LocationSource.Listener) {
        this.listener = listener
        request(plan)
    }

    override fun update(plan: SamplingPlan) {
        if (listener != null && plan != this.plan) request(plan)
    }

    @SuppressLint("MissingPermission")
    private fun request(plan: SamplingPlan) {
        this.plan = plan
        lm.removeUpdates(ll)
        val provider = if (lm.isProviderEnabled(LocationManager.GPS_PROVIDER)) LocationManager.GPS_PROVIDER else LocationManager.NETWORK_PROVIDER
        if (!lm.isProviderEnabled(provider)) {
            listener?.onAvailabilityChanged(false)
            return
        }
        lm.requestLocationUpdates(provider, plan.intervalMs, plan.minDistanceM, ll, Looper.getMainLooper())
    }

    override fun stop() {
        lm.removeUpdates(ll)
        listener = null
        plan = null
    }
}
