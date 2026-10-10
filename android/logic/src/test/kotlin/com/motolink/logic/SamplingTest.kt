package com.motolink.logic

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SamplingTest {
    @Test fun startsInMovingProfile() {
        val p = AdaptiveSampler().current
        assertEquals(MotionState.MOVING, p.state)
        assertEquals(3_000, p.intervalMs)
        assertTrue(p.highAccuracy)
    }

    @Test fun redLightDoesNotFlipProfile() {
        val s = AdaptiveSampler()
        var t = 0L
        repeat(5) { s.onFix(t, 12f); t += 3_000 }
        // stop for 10 s only: below stillAfterMs
        repeat(3) { assertNull(s.onFix(t, 0f)); t += 3_000 }
        assertEquals(MotionState.MOVING, s.current.state, "10 s at a red light keeps the moving profile")
    }

    @Test fun settlesToStillThenStillLong() {
        val s = AdaptiveSampler()
        var t = 0L
        repeat(5) { s.onFix(t, 12f); t += 3_000 }
        var reachedStill: SamplingPlan? = null
        var reachedLong: SamplingPlan? = null
        repeat(60) {
            val ch = s.onFix(t, 0f)
            if (ch?.state == MotionState.STILL) reachedStill = ch
            if (ch?.state == MotionState.STILL_LONG) reachedLong = ch
            t += 3_000
        }
        assertEquals(20_000, reachedStill?.intervalMs)
        assertFalse(reachedStill!!.highAccuracy)
        assertEquals(30_000, reachedLong?.intervalMs)
    }

    @Test fun movingIsRecognisedQuicklyAfterStandstill() {
        val s = AdaptiveSampler()
        var t = 0L
        repeat(50) { s.onFix(t, 0f); t += 3_000 }
        assertEquals(MotionState.STILL_LONG, s.current.state)
        var back: SamplingPlan? = null
        repeat(6) { s.onFix(t, 10f)?.let { back = it }; t += 3_000 }
        assertEquals(MotionState.MOVING, s.current.state)
        assertEquals(3_000, back?.intervalMs)
    }

    @Test fun nullSpeedKeepsPreviousEstimate() {
        val s = AdaptiveSampler()
        s.onFix(0, 12f)
        assertNull(s.onFix(3_000, null))
        assertEquals(MotionState.MOVING, s.current.state)
    }
}

class QualityFilterTest {
    private fun f(lat: Double, lng: Double, ts: Long, acc: Float? = 8f, speed: Float? = 5f) =
        Fix(lat, lng, acc, speed, null, null, ts)

    @Test fun acceptsGoodFix() {
        assertEquals(FixDecision.Accept(false), QualityFilter().evaluate(f(11.94, 108.45, 1_000)))
    }

    @Test fun rejectsBadAccuracyAndCoordinates() {
        val q = QualityFilter()
        assertEquals(FixDecision.Reject("low_accuracy"), q.evaluate(f(11.94, 108.45, 1_000, acc = 120f)))
        assertEquals(FixDecision.Reject("low_accuracy"), q.evaluate(f(11.94, 108.45, 1_000, acc = null)))
        assertEquals(FixDecision.Reject("invalid_coordinates"), q.evaluate(f(0.0, 0.0, 1_000)))
    }

    @Test fun rejectsOutOfOrderAndJumps() {
        val q = QualityFilter()
        q.evaluate(f(11.94, 108.45, 10_000))
        assertEquals(FixDecision.Reject("out_of_order"), q.evaluate(f(11.94, 108.45, 9_000)))
        // ~111 km in 3 s
        assertEquals(FixDecision.Reject("jump"), q.evaluate(f(12.94, 108.45, 13_000)))
        // a rejected fix must not become the new reference
        assertTrue(q.evaluate(f(11.9401, 108.45, 13_000)) is FixDecision.Accept)
    }

    @Test fun flagsImplausibleSpeedAsSuspectButKeepsIt() {
        assertEquals(FixDecision.Accept(true), QualityFilter().evaluate(f(11.94, 108.45, 1_000, speed = 80f)))
    }
}
