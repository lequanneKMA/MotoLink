package com.motolink.logic

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class NavTest {
    @Test fun polylineDecodesTheReferenceExample() {
        val pts = Polyline.decode("_p~iF~ps|U_ulLnnqC_mqNvxq`@", precision = 5)
        assertEquals(3, pts.size)
        assertEquals(38.5, pts[0].lat, 1e-9); assertEquals(-120.2, pts[0].lng, 1e-9)
        assertEquals(40.7, pts[1].lat, 1e-9); assertEquals(-120.95, pts[1].lng, 1e-9)
        assertEquals(43.252, pts[2].lat, 1e-9); assertEquals(-126.453, pts[2].lng, 1e-9)
    }

    @Test fun polylineRoundTripsAtPrecision6() {
        val src = listOf(GeoPoint(10.776889, 106.700806), GeoPoint(10.7801, 106.6992), GeoPoint(10.7799, 106.7041))
        val back = Polyline.decode(Polyline.encode(src, 6), 6)
        assertEquals(src.size, back.size)
        src.zip(back).forEach { (a, b) -> assertEquals(a.lat, b.lat, 1e-6); assertEquals(a.lng, b.lng, 1e-6) }
    }

    @Test fun truncatedPolylineIsRejected() {
        var failed = false
        try { Polyline.decode("_p~iF~ps|U_ulL", 5) } catch (e: IllegalArgumentException) { failed = true }
        assertTrue(failed)
    }

    private fun step(type: String, mod: String = "", name: String = "", exit: Int = 0, at: GeoPoint = GeoPoint(0.0, 0.0), d: Double = 0.0) =
        NavStep(d, 0.0, name, type, mod, exit, at)

    @Test fun vietnameseInstructions() {
        assertEquals("Rẽ phải vào Nguyễn Đình Chiểu", NavText.instruction(step("turn", "right", "Nguyễn Đình Chiểu")))
        assertEquals("Rẽ trái", NavText.instruction(step("turn", "left")))
        assertEquals("Quay đầu", NavText.instruction(step("turn", "uturn")))
        assertEquals("Chếch sang phải vào QL 20", NavText.instruction(step("turn", "slight right", "QL 20")))
        assertEquals("Đi nhánh bên trái", NavText.instruction(step("fork", "slight left")))
        assertEquals("Cuối đường, rẽ phải", NavText.instruction(step("end of road", "right", "Lê Lợi")))
        assertEquals("Vào bùng binh, ra lối thứ 2 vào Hai Bà Trưng", NavText.instruction(step("roundabout", "right", "Hai Bà Trưng", exit = 2)))
        assertEquals("Đã đến nơi: Chợ Bến Thành", NavText.instruction(step("arrive", "", "Chợ Bến Thành")))
        assertEquals("Đi thẳng", NavText.instruction(step("continue", "straight")))
        assertEquals(Maneuver.SHARP_LEFT, NavText.maneuver(step("turn", "sharp left")))
    }

    @Test fun spokenAdvanceWarning() {
        val s = step("turn", "right", "Võ Văn Tần")
        assertEquals("Sau 500 mét, rẽ phải vào Võ Văn Tần", NavText.spoken(s, 500.0))
        assertEquals("Sau 150 mét, rẽ phải vào Võ Văn Tần", NavText.spoken(s, 152.0))
        assertEquals("Rẽ phải vào Võ Văn Tần", NavText.spoken(s, 10.0))
        assertEquals("Sau 1,2 ki-lô-mét, rẽ phải vào Võ Văn Tần", NavText.spoken(s, 1200.0))
    }

    // A straight road due east along lat 10.0 from lng 106.000 to 106.020 (~2.19 km), with a right turn at 106.010.
    private val line = (0..20).map { GeoPoint(10.0, 106.0 + it * 0.001) }
    private val route = NavRoute(
        line,
        listOf(
            step("depart", "right", "A", at = line[0]),
            step("turn", "right", "B", at = GeoPoint(10.0, 106.010)),
            step("arrive", "", "Đích", at = line.last()),
        ),
        durationS = 300.0,
    )

    @Test fun stepsAreLocatedAlongTheRoute() {
        assertEquals(0.0, route.stepStartM[0], 1e-6)
        assertEquals(route.totalM / 2, route.stepStartM[1], 5.0)
        assertEquals(route.totalM, route.stepStartM[2], 1e-6)
        assertEquals(2190.0, route.totalM, 15.0)
    }

    @Test fun progressRemainingAndNextTurn() {
        val t = NavTracker(route)
        val s = t.update(10.0, 106.0035) // ~385 m along, a few metres off the line is fine
        assertEquals(385.0, s.alongM, 10.0)
        assertEquals(1, s.nextStep)
        assertEquals(route.stepStartM[1] - s.alongM, s.distToNextM, 1.0)
        assertEquals(route.totalM - s.alongM, s.remainingM, 1.0)
        assertEquals(300.0 * s.remainingM / route.totalM, s.remainingS, 0.5)
        assertFalse(s.offRoute); assertFalse(s.arrived)
        val after = t.update(10.0, 106.0105) // past the turn: the next instruction is the arrival
        assertEquals(2, after.nextStep)
    }

    @Test fun leavingTheRouteNeedsSeveralFixes() {
        val t = NavTracker(route)
        t.update(10.0, 106.002)
        assertFalse(t.update(10.002, 106.003).offRoute)   // ~220 m north, first fix
        assertFalse(t.update(10.002, 106.004).offRoute)   // second
        assertTrue(t.update(10.002, 106.005).offRoute)    // third consecutive: reroute
        assertFalse(t.update(10.0, 106.006).offRoute)     // back on the line: counter resets
    }

    @Test fun arrivalIsDetected() {
        val s = NavTracker(route).update(10.0, 106.01995)
        assertTrue(s.arrived)
        assertNull(s.nextStep)
    }

    @Test fun voicePromptsAreSpokenOncePerThreshold() {
        val t = NavTracker(route)
        val said = ArrayList<String>()
        for (lng in listOf(106.0003, 106.0020, 106.0053, 106.0054, 106.0079, 106.0080, 106.0089, 106.0097, 106.0099)) {
            t.prompt(t.update(10.0, lng))?.let { said += it }
        }
        // turn at 106.010: 500 m ≈ 106.0055, 150 m ≈ 106.0087, at the turn ≈ 106.0097
        assertEquals(
            listOf("Sau 500 mét, rẽ phải vào B", "Sau 150 mét, rẽ phải vào B", "Rẽ phải vào B"),
            said.filterNot { it.contains("Bắt đầu") },
        )
        assertNotNull(said)
    }

    @Test fun formatting() {
        assertEquals("12 phút", NavFormat.duration(720.0))
        assertEquals("1 giờ 5 phút", NavFormat.duration(3900.0))
        assertEquals("1 phút", NavFormat.duration(10.0))
        assertEquals("07:30", NavFormat.arrival(1800.0, 0, 7 * 3600_000L))
    }
}
