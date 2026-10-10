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
