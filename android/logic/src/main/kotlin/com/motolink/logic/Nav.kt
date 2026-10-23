package com.motolink.logic

import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.min

/** A WGS84 point (kept separate from Android/MapLibre types so the navigation maths is testable on the JVM). */
data class GeoPoint(val lat: Double, val lng: Double)

/** Google/OSRM encoded polyline. OSRM `geometries=polyline6` uses precision 6. */
object Polyline {
    fun decode(encoded: String, precision: Int = 6): List<GeoPoint> {
        val factor = Math.pow(10.0, precision.toDouble())
        val out = ArrayList<GeoPoint>()
        var i = 0
        var lat = 0
        var lng = 0
        fun next(): Int {
            var result = 0
            var shift = 0
            var b: Int
            do {
                require(i < encoded.length) { "truncated polyline" }
                b = encoded[i++].code - 63
                result = result or ((b and 0x1f) shl shift)
                shift += 5
            } while (b >= 0x20)
            return if (result and 1 != 0) (result shr 1).inv() else result shr 1
        }
        while (i < encoded.length) {
            lat += next()
            lng += next()
            out += GeoPoint(lat / factor, lng / factor)
        }
        return out
    }

    fun encode(points: List<GeoPoint>, precision: Int = 6): String {
        val factor = Math.pow(10.0, precision.toDouble())
        val sb = StringBuilder()
        var pLat = 0
        var pLng = 0
        fun put(v: Int) {
            var x = if (v < 0) (v shl 1).inv() else v shl 1
            while (x >= 0x20) {
                sb.append((((x and 0x1f) or 0x20) + 63).toChar())
                x = x shr 5
            }
            sb.append((x + 63).toChar())
        }
        for (p in points) {
            val la = Math.round(p.lat * factor).toInt()
            val ln = Math.round(p.lng * factor).toInt()
            put(la - pLat)
            put(ln - pLng)
            pLat = la
            pLng = ln
        }
        return sb.toString()
    }
}

enum class Maneuver { DEPART, STRAIGHT, SLIGHT_LEFT, LEFT, SHARP_LEFT, SLIGHT_RIGHT, RIGHT, SHARP_RIGHT, UTURN, ROUNDABOUT, MERGE, ARRIVE }

/** One instruction as sent by the server (OSRM vocabulary). */
data class NavStep(
    val distanceM: Double,
    val durationS: Double,
    val name: String,
    val type: String,
    val modifier: String,
    val exit: Int,
    val at: GeoPoint,
)

/** Vietnamese wording and icons for OSRM manoeuvres. */
object NavText {
    fun maneuver(s: NavStep): Maneuver = when (s.type) {
        "depart" -> Maneuver.DEPART
        "arrive" -> Maneuver.ARRIVE
        "roundabout", "rotary", "roundabout turn" -> Maneuver.ROUNDABOUT
        "merge" -> Maneuver.MERGE
        else -> when (s.modifier) {
            "left" -> Maneuver.LEFT
            "right" -> Maneuver.RIGHT
            "slight left" -> Maneuver.SLIGHT_LEFT
            "slight right" -> Maneuver.SLIGHT_RIGHT
            "sharp left" -> Maneuver.SHARP_LEFT
            "sharp right" -> Maneuver.SHARP_RIGHT
            "uturn" -> Maneuver.UTURN
            else -> Maneuver.STRAIGHT
        }
    }

    private fun into(name: String) = if (name.isBlank()) "" else " vào $name"

    /** "Rẽ phải vào Nguyễn Đình Chiểu" */
    fun instruction(s: NavStep): String = when (maneuver(s)) {
        Maneuver.DEPART -> if (s.name.isBlank()) "Bắt đầu đi" else "Bắt đầu đi trên ${s.name}"
        Maneuver.ARRIVE -> "Đã đến nơi" + if (s.name.isBlank()) "" else ": ${s.name}"
        Maneuver.ROUNDABOUT -> if (s.exit > 0) "Vào bùng binh, ra lối thứ ${s.exit}" + into(s.name) else "Đi qua bùng binh"
        Maneuver.MERGE -> "Nhập làn" + into(s.name)
        Maneuver.LEFT -> (if (s.type == "end of road") "Cuối đường, rẽ trái" else "Rẽ trái") + into(s.name).takeIf { s.type != "end of road" }.orEmpty()
        Maneuver.RIGHT -> (if (s.type == "end of road") "Cuối đường, rẽ phải" else "Rẽ phải") + into(s.name).takeIf { s.type != "end of road" }.orEmpty()
        Maneuver.SLIGHT_LEFT -> (if (s.type == "fork") "Đi nhánh bên trái" else "Chếch sang trái") + into(s.name)
        Maneuver.SLIGHT_RIGHT -> (if (s.type == "fork") "Đi nhánh bên phải" else "Chếch sang phải") + into(s.name)
        Maneuver.SHARP_LEFT -> "Rẽ gắt sang trái" + into(s.name)
        Maneuver.SHARP_RIGHT -> "Rẽ gắt sang phải" + into(s.name)
        Maneuver.UTURN -> "Quay đầu"
        Maneuver.STRAIGHT -> if (s.name.isBlank()) "Đi thẳng" else "Đi thẳng vào ${s.name}"
    }

    /** Sentence for the speech engine: "Sau 400 mét, rẽ phải vào ..."; right at the manoeuvre just the instruction. */
    fun spoken(s: NavStep, distanceM: Double): String {
        val ins = instruction(s)
        if (distanceM < 40) return ins
        val d = if (distanceM >= 950) "${Math.round(distanceM / 100.0) / 10.0} ki-lô-mét".replace('.', ',') else "${(Math.round(distanceM / 50.0) * 50).toInt()} mét"
        return "Sau $d, " + ins.replaceFirstChar { it.lowercase() }
    }
}

/** A route ready for tracking: vertices, cumulative distances and the along-route position of every manoeuvre. */
class NavRoute(val points: List<GeoPoint>, val steps: List<NavStep>, val durationS: Double) {
    val cum: DoubleArray = DoubleArray(points.size).also { c ->
        for (i in 1 until points.size) c[i] = c[i - 1] + Geo.haversineMeters(points[i - 1].lat, points[i - 1].lng, points[i].lat, points[i].lng)
    }
    val totalM: Double get() = if (cum.isEmpty()) 0.0 else cum.last()

    /** Along-route distance at which each step's manoeuvre happens (non-decreasing). */
    val stepStartM: DoubleArray = DoubleArray(steps.size).also { out ->
        var from = 0
        for ((k, s) in steps.withIndex()) {
            var best = from
            var bestD = Double.MAX_VALUE
            for (j in from until points.size) {
                val d = Geo.haversineMeters(points[j].lat, points[j].lng, s.at.lat, s.at.lng)
                if (d < bestD) { bestD = d; best = j }
                if (cum[j] - cum[from] > 0 && d > bestD + 500) break // moving away: the nearest vertex is behind us
            }
            from = best
            out[k] = cum[best]
        }
    }
}

data class NavState(
    val alongM: Double,
    val offRouteM: Double,
    val remainingM: Double,
    val remainingS: Double,
    /** Index of the step whose manoeuvre comes next (null once arrived). */
    val nextStep: Int?,
    val distToNextM: Double,
    val arrived: Boolean,
    val offRoute: Boolean,
)

/**
 * Follows a rider along a [NavRoute]: snaps each fix to the line, reports what is left and what comes next, and
 * says when the rider left the route (a few consecutive fixes far from it) so the caller can ask for a new route.
 */
class NavTracker(val route: NavRoute, private val offRouteM: Double = 45.0, private val offRouteFixes: Int = 3, private val arriveM: Double = 25.0) {
    private var seg = 0
    private var far = 0
    private val announced = HashMap<Int, Int>() // step -> lowest threshold index already spoken

    fun update(lat: Double, lng: Double): NavState {
        val pts = route.points
        if (pts.size < 2) return NavState(0.0, 0.0, 0.0, 0.0, null, 0.0, arrived = true, offRoute = false)
        // local search first (cheap, keeps us from jumping to a parallel road), then the whole route
        var best = nearest(lat, lng, max(0, seg - 2), min(pts.size - 2, seg + 80))
        if (best.dist > offRouteM) {
            val all = nearest(lat, lng, 0, pts.size - 2)
            if (all.dist < best.dist - 15) best = all
        }
        seg = best.seg
        val along = route.cum[best.seg] + best.t * (route.cum[best.seg + 1] - route.cum[best.seg])
        far = if (best.dist > offRouteM) far + 1 else 0
        val remaining = (route.totalM - along).coerceAtLeast(0.0)
        val arrived = remaining <= arriveM
        val next = (0 until route.steps.size).firstOrNull { route.stepStartM[it] > along + 5 }
        return NavState(
            alongM = along, offRouteM = best.dist, remainingM = remaining,
            remainingS = if (route.totalM > 0) route.durationS * remaining / route.totalM else 0.0,
            nextStep = if (arrived) null else next, distToNextM = next?.let { (route.stepStartM[it] - along).coerceAtLeast(0.0) } ?: remaining,
            arrived = arrived, offRoute = far >= offRouteFixes && !arrived,
        )
    }

    /** Voice prompt due now, at most once per threshold per step: 500 m, 150 m, and at the turn itself. */
    fun prompt(s: NavState): String? {
        val idx = s.nextStep ?: return null
        val step = route.steps[idx]
        val thresholds = intArrayOf(500, 150, 35)
        val level = thresholds.indexOfLast { s.distToNextM <= it }
        if (level < 0) return null
        val done = announced[idx] ?: -1
        if (level <= done) return null
        announced[idx] = level
        return NavText.spoken(step, if (level == 2) 0.0 else thresholds[level].toDouble())
    }

    private class Hit(val seg: Int, val t: Double, val dist: Double)

    private fun nearest(lat: Double, lng: Double, from: Int, to: Int): Hit {
        val pts = route.points
        val k = cos(Math.toRadians(lat))
        var best = Hit(from, 0.0, Double.MAX_VALUE)
        for (i in from..to) {
            val ax = (pts[i].lng - lng) * k * 111_320.0
            val ay = (pts[i].lat - lat) * 110_574.0
            val bx = (pts[i + 1].lng - lng) * k * 111_320.0
            val by = (pts[i + 1].lat - lat) * 110_574.0
            val dx = bx - ax
            val dy = by - ay
            val len2 = dx * dx + dy * dy
            val t = if (len2 == 0.0) 0.0 else (-(ax * dx + ay * dy) / len2).coerceIn(0.0, 1.0)
            val d = hypot(ax + t * dx, ay + t * dy)
            if (d < best.dist) best = Hit(i, t, d)
        }
        return best
    }
}

/** Seconds → "12 phút", "1 giờ 5 phút"; metres → "4,3 km" (for the bottom bar and route options). */
object NavFormat {
    fun duration(s: Double): String {
        val m = Math.round(s / 60.0).toInt().coerceAtLeast(1)
        return if (m < 60) "$m phút" else "${m / 60} giờ ${m % 60} phút"
    }

    fun arrival(remainingS: Double, nowMs: Long, zoneOffsetMs: Long): String {
        val t = ((nowMs + zoneOffsetMs + (remainingS * 1000).toLong()) / 60_000) % (24 * 60)
        return String.format(java.util.Locale.US, "%02d:%02d", t / 60, t % 60)
    }

    fun distance(m: Double): String = RideText.distance(m)
}
