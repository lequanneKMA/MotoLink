package com.motolink.logic

import kotlin.math.PI
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.sin
import kotlin.math.sqrt

/** A GPS fix as produced by a location source, before it is given a sequence number. */
data class Fix(
    val lat: Double,
    val lng: Double,
    val accuracyM: Float?,
    val speedMps: Float?,
    val headingDeg: Float?,
    val altitudeM: Double?,
    /** Device clock, epoch millis. Never used to order points (ADR-004). */
    val clientTsMs: Long,
)

object Geo {
    private const val EARTH_RADIUS_M = 6_371_008.8

    fun haversineMeters(lat1: Double, lng1: Double, lat2: Double, lng2: Double): Double {
        val p1 = Math.toRadians(lat1)
        val p2 = Math.toRadians(lat2)
        val dp = p2 - p1
        val dl = Math.toRadians(lng2 - lng1)
        val a = sin(dp / 2) * sin(dp / 2) + cos(p1) * cos(p2) * sin(dl / 2) * sin(dl / 2)
        return 2 * EARTH_RADIUS_M * atan2(sqrt(a), sqrt(max(0.0, 1 - a)))
    }

    fun distance(a: Fix, b: Fix): Double = haversineMeters(a.lat, a.lng, b.lat, b.lng)

    fun validCoordinates(lat: Double, lng: Double): Boolean =
        lat.isFinite() && lng.isFinite() && lat in -90.0..90.0 && lng in -180.0..180.0 && !(lat == 0.0 && lng == 0.0)

    /** Local equirectangular projection (metres) around a reference latitude; accurate enough for route thinning. */
    internal fun toXY(lat: Double, lng: Double, refLat: Double): Pair<Double, Double> {
        val x = Math.toRadians(lng) * EARTH_RADIUS_M * cos(Math.toRadians(refLat))
        val y = Math.toRadians(lat) * EARTH_RADIUS_M
        return x to y
    }

    @Suppress("unused")
    internal const val DEG = 180.0 / PI
}

/** Ramer–Douglas–Peucker thinning, used when the local buffer overflows and for route display. */
object Rdp {
    /** Returns the indices of [points] to keep, always including first and last. */
    fun simplifyIndices(points: List<Fix>, epsilonM: Double): List<Int> {
        if (points.size <= 2) return points.indices.toList()
        val refLat = points.sumOf { it.lat } / points.size
        val xy = points.map { Geo.toXY(it.lat, it.lng, refLat) }
        val keep = BooleanArray(points.size)
        keep[0] = true
        keep[points.lastIndex] = true
        // iterative stack: no recursion depth risk on long tracks
        val stack = ArrayDeque<Pair<Int, Int>>()
        stack.addLast(0 to points.lastIndex)
        while (stack.isNotEmpty()) {
            val (s, e) = stack.removeLast()
            if (e <= s + 1) continue
            var maxD = -1.0
            var idx = -1
            for (i in s + 1 until e) {
                val d = perpendicular(xy[i], xy[s], xy[e])
                if (d > maxD) {
                    maxD = d
                    idx = i
                }
            }
            if (maxD > epsilonM && idx >= 0) {
                keep[idx] = true
                stack.addLast(s to idx)
                stack.addLast(idx to e)
            }
        }
        return keep.indices.filter { keep[it] }
    }

    private fun perpendicular(p: Pair<Double, Double>, a: Pair<Double, Double>, b: Pair<Double, Double>): Double {
        val dx = b.first - a.first
        val dy = b.second - a.second
        val len2 = dx * dx + dy * dy
        if (len2 == 0.0) return hypot(p.first - a.first, p.second - a.second)
        val t = ((p.first - a.first) * dx + (p.second - a.second) * dy) / len2
        val tc = t.coerceIn(0.0, 1.0)
        return hypot(p.first - (a.first + tc * dx), p.second - (a.second + tc * dy))
    }

    private fun hypot(x: Double, y: Double) = sqrt(x * x + y * y)
}

data class RouteStats(val distanceM: Double, val durationS: Long, val avgSpeedMps: Double, val pointCount: Int)

object RouteMath {
    /** Distance over [fixes] in order, ignoring implausible jumps (> [maxJumpMps]). */
    fun stats(fixes: List<Fix>, maxJumpMps: Double = 90.0): RouteStats {
        if (fixes.size < 2) return RouteStats(0.0, 0, 0.0, fixes.size)
        var dist = 0.0
        for (i in 1 until fixes.size) {
            val d = Geo.distance(fixes[i - 1], fixes[i])
            val dt = (fixes[i].clientTsMs - fixes[i - 1].clientTsMs) / 1000.0
            if (dt > 0 && d / dt > maxJumpMps) continue
            dist += d
        }
        val dur = max(0L, (fixes.last().clientTsMs - fixes.first().clientTsMs) / 1000)
        return RouteStats(dist, dur, if (dur > 0) dist / dur else 0.0, fixes.size)
    }
}
