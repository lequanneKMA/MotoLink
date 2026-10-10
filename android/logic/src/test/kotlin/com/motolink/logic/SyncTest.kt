package com.motolink.logic

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class PositionOrderTest {
    @Test fun sessionFirstThenSeq() {
        val s1 = "018f0000-0000-7000-8000-000000000001"
        val s2 = "018f0000-0000-7000-8000-000000000002"
        assertTrue(PositionKey(s1, 5) > PositionKey(s1, 3))
        assertTrue(PositionKey(s2, 1) > PositionKey(s1, 999), "newer session wins regardless of seq")
        assertFalse(PositionKey(s1, 3).isNewerThan(PositionKey(s1, 3)), "equal is not newer")
        assertTrue(PositionKey(s1, 1).isNewerThan(null))
    }

    @Test fun reconcilerNeverMovesBackwards() { // docs/design/14 §3.1 scenarios, client side
        val r = LocationReconciler<String>()
        val s1 = "018f0000-0000-7000-8000-000000000001"
        val s2 = "018f0000-0000-7000-8000-000000000002"
        assertTrue(r.apply("u", PositionKey(s1, 5), "p5"))
        assertFalse(r.apply("u", PositionKey(s1, 3), "p3"), "older seq")
        assertFalse(r.apply("u", PositionKey(s1, 5), "dup"), "duplicate")
        assertEquals("p5", r.get("u"))
        assertTrue(r.apply("u", PositionKey(s2, 1), "new session"))
        assertFalse(r.apply("u", PositionKey(s1, 100), "old session late"))
        assertEquals("new session", r.get("u"))
        assertTrue(r.apply("other", PositionKey(s1, 1), "o"), "users are independent")
    }
}

class EventCursorTest {
    @Test fun appliesContiguousAndDetectsDuplicatesAndGaps() {
        val c = EventCursor(41)
        assertEquals(CursorDecision.Apply, c.offer(42))
        assertEquals(CursorDecision.Duplicate, c.offer(42))
        assertEquals(CursorDecision.Duplicate, c.offer(10))
        assertEquals(CursorDecision.Gap(43, 44), c.offer(45))
        assertEquals(42, c.lastSeq, "a gap must not advance the cursor")
        c.advanceTo(45)
        assertEquals(CursorDecision.Apply, c.offer(46))
        c.advanceTo(3)
        assertEquals(46, c.lastSeq, "never backwards")
    }
}

class PendingTrackerTest {
    @Test fun ackRemovesEverythingUpTo() {
        val p = PendingTracker()
        (1L..10L).forEach(p::add)
        assertEquals((1L..7L).toList(), p.onAck(7))
        assertEquals((8L..10L).toList(), p.seqs())
        assertEquals(emptyList(), p.onAck(7), "repeated ack is harmless")
        assertEquals(listOf(8L, 9L, 10L), p.onAck(500))
        assertEquals(0, p.size)
    }

    @Test fun resendOnlyAfterTimeoutOrReconnect() {
        val p = PendingTracker(resendAfterMs = 5_000)
        (1L..5L).forEach(p::add)
        assertEquals(listOf(1L, 2L, 3L), p.due(0, 3))
        p.markSent(listOf(1L, 2L, 3L), 0)
        assertEquals(listOf(4L, 5L), p.due(1_000, 10), "sent points wait for ACK")
        p.markSent(listOf(4L, 5L), 1_000)
        assertEquals(emptyList(), p.due(2_000, 10))
        assertEquals(listOf(1L, 2L, 3L), p.due(5_000, 10), "timeout => resend oldest first")
        p.onReconnect()
        assertEquals(listOf(1L, 2L, 3L, 4L, 5L), p.due(5_001, 10), "reconnect resends everything unacked")
    }

    @Test fun addIsIdempotent() {
        val p = PendingTracker()
        p.add(1); p.markSent(listOf(1), 100); p.add(1)
        assertEquals(1, p.size)
        assertEquals(emptyList(), p.due(101, 5), "re-adding must not reset send state")
    }
}

class BackoffTest {
    @Test fun growsToCapWithJitterBounds() {
        val low = Backoff(random = { 0.0 })   // jitter factor 0.5
        val high = Backoff(random = { 0.999 }) // ~1.5
        val lows = List(8) { low.nextDelayMs() }
        val highs = List(8) { high.nextDelayMs() }
        assertEquals(500, lows[0])
        assertEquals(1_000, lows[1])
        assertTrue(highs.all { it <= 45_000 }, "cap 30s * 1.5")
        assertTrue(lows.last() in 14_000..16_000, "30s cap * 0.5")
        low.reset()
        assertEquals(500, low.nextDelayMs())
    }
}

class PresenceClassifierTest {
    private val now = 1_000_000L
    @Test fun statuses() {
        fun c(online: Boolean = true, sharing: Boolean = true, last: Long? = now - 5_000, off: Long = 0) =
            PresenceClassifier.classify(online, sharing, last, now, off)
        assertEquals(MemberStatus.LIVE, c())
        assertEquals(MemberStatus.STALE, c(last = now - 45_000))
        assertEquals(MemberStatus.LOST, c(last = now - 120_000))
        assertEquals(MemberStatus.LOST, c(last = null))
        assertEquals(MemberStatus.OFFLINE, c(online = false))
        assertEquals(MemberStatus.SHARING_HIDDEN, c(sharing = false, online = false))
    }

    @Test fun clockOffsetCorrectsSkewedDevice() {
        // device clock is 60s behind the server (offset = +60s): a point 5s old server-time is LIVE
        val serverNow = now + 60_000
        val ts = serverNow - 5_000
        assertEquals(MemberStatus.LIVE, PresenceClassifier.classify(true, true, ts, now, clockOffsetMs = 60_000))
        // the same point seen 100s later is LOST once the offset is applied
        assertEquals(MemberStatus.LOST, PresenceClassifier.classify(true, true, ts, now + 100_000, clockOffsetMs = 60_000))
    }
}

class TrackingMachineTest {
    @Test fun happyPath() {
        var s = TrackState.IDLE
        fun step(e: TrackEvent) { s = checkNotNull(TrackingMachine.next(s, e)) { "$e not allowed in $s" } }
        step(TrackEvent.UserStart); assertEquals(TrackState.STARTING, s)
        step(TrackEvent.ServerSessionCreated); assertEquals(TrackState.ACTIVE, s)
        assertTrue(TrackingMachine.gpsAllowed(s))
        step(TrackEvent.UserPause); assertFalse(TrackingMachine.gpsAllowed(s))
        step(TrackEvent.UserResume); assertTrue(TrackingMachine.gpsAllowed(s))
        step(TrackEvent.UserEnd); assertEquals(TrackState.ENDED, s)
        step(TrackEvent.UserStart) // may start again after ending
    }

    @Test fun forbiddenTransitions() {
        assertNull(TrackingMachine.next(TrackState.IDLE, TrackEvent.UserPause))
        assertNull(TrackingMachine.next(TrackState.IDLE, TrackEvent.UserEnd))
        assertNull(TrackingMachine.next(TrackState.ACTIVE, TrackEvent.UserStart))
        assertNull(TrackingMachine.next(TrackState.PAUSED, TrackEvent.ServerSessionCreated))
        assertEquals(TrackState.IDLE, TrackingMachine.next(TrackState.STARTING, TrackEvent.StartFailed))
        assertEquals(TrackState.ENDED, TrackingMachine.next(TrackState.PAUSED, TrackEvent.ForcedEnd))
        assertNull(TrackingMachine.next(TrackState.IDLE, TrackEvent.ForcedEnd))
        assertFalse(TrackingMachine.gpsAllowed(TrackState.STARTING))
    }
}
