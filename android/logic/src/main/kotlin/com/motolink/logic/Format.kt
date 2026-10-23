package com.motolink.logic

import java.util.Locale

/** Display formatting that must not depend on the device locale (decimal point, digits). */
object Format {
    fun distance(m: Double): String =
        if (m < 1000) "${m.toInt().coerceAtLeast(0)} m" else String.format(Locale.US, "%.2f km", m / 1000)

    fun duration(totalSeconds: Long): String {
        val s = totalSeconds.coerceAtLeast(0)
        val h = s / 3600
        val m = (s % 3600) / 60
        val sec = s % 60
        return if (h > 0) String.format(Locale.US, "%d:%02d:%02d", h, m, sec) else String.format(Locale.US, "%d:%02d", m, sec)
    }

    fun speedKmh(mps: Double): String = String.format(Locale.US, "%.1f km/h", mps * 3.6)
}

/** Wording shown while riding. Vietnamese decimal comma, short units; independent of the device locale. */
object RideText {
    /** "850 m", "1,2 km", "12 km" */
    fun distance(m: Double): String = when {
        m < 1000 -> "${(Math.round(m / 10.0) * 10).coerceAtLeast(0)} m".let { if (m < 100) "${m.toInt().coerceAtLeast(0)} m" else it }
        m < 10_000 -> String.format(Locale.US, "%.1f km", m / 1000).replace('.', ',')
        else -> "${Math.round(m / 1000.0)} km"
    }

    /** "30 giây", "2 phút", "1 giờ 5 phút" */
    fun silence(seconds: Long): String {
        val s = seconds.coerceAtLeast(0)
        return when {
            s < 60 -> "$s giây"
            s < 3600 -> "${s / 60} phút"
            else -> "${s / 3600} giờ ${(s % 3600) / 60} phút"
        }
    }
}
