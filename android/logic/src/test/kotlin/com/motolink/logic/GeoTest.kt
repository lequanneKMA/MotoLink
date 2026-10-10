package com.motolink.logic

import kotlin.math.abs
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

private fun fix(lat: Double, lng: Double, ts: Long, acc: Float? = 5f, speed: Float? = null) =
    Fix(lat, lng, acc, speed, null, null, ts)

class GeoTest {
    @Test fun haversineKnownDistance() {
        // Ha Noi -> Ho Chi Minh City is about 1,138 km great-circle
        val d = Geo.haversineMeters(21.0278, 105.8342, 10.8231, 106.6297)
        assertTrue(abs(d - 1_138_000) < 15_000, "got $d")
        assertEquals(0.0, Geo.haversineMeters(11.9, 108.4, 11.9, 108.4), 1e-6)
    }

    @Test fun coordinateValidation() {
        assertTrue(Geo.validCoordinates(11.94, 108.45))
        assertFalse(Geo.validCoordinates(91.0, 0.0))
        assertFalse(Geo.validCoordinates(0.0, 181.0))
        assertFalse(Geo.validCoordinates(0.0, 0.0), "null island")
        assertFalse(Geo.validCoordinates(Double.NaN, 1.0))
    }

    @Test fun rdpKeepsCornersDropsStraightRun() {
        // straight east for 10 points, then straight north for 10 points
        val pts = ArrayList<Fix>()
        for (i in 0..10) pts += fix(11.0, 108.0 + i * 0.0001, i.toLong())
        for (j in 1..10) pts += fix(11.0 + j * 0.0001, 108.001, (10 + j).toLong())
        val keep = Rdp.simplifyIndices(pts, 2.0)
        assertTrue(0 in keep && pts.lastIndex in keep)
        assertTrue(10 in keep, "the corner must be kept: $keep")
        assertTrue(keep.size <= 4, "straight runs collapse: $keep")
    }

    @Test fun rdpHandlesTinyAndDegenerateInput() {
        assertEquals(listOf(0), Rdp.simplifyIndices(listOf(fix(11.0, 108.0, 0)), 5.0))
        val same = List(5) { fix(11.0, 108.0, it.toLong()) }
        assertEquals(listOf(0, 4), Rdp.simplifyIndices(same, 5.0))
    }

    @Test fun routeStatsIgnoreTeleports() {
        val a = fix(11.0000, 108.0000, 0)
        val b = fix(11.0010, 108.0000, 10_000)        // ~111 m in 10 s
        val teleport = fix(12.0000, 108.0000, 11_000) // ~111 km in 1 s: skipped
        val c = fix(12.0010, 108.0000, 21_000)
        val s = RouteMath.stats(listOf(a, b, teleport, c))
        assertTrue(s.distanceM in 100.0..260.0, "teleport must not count: ${s.distanceM}")
        assertEquals(21, s.durationS)
    }
}
