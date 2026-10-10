package com.motolink.logic

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class TripLiveTest {
    private fun snap(head: Long, replayCount: Int = 0, from: Long = head + 1, vararg ms: SnapshotMember) =
        TripJoinedPayload(TripBrief("active", "Đà Lạt"), head, ReplayInfo(from, replayCount, true), ms.toList())

    private fun member(id: String, sharing: String = "sharing", presence: String = "online", loc: MemberLocation? = null) =
        SnapshotMember(id, "U-$id", "#fff", "member", sharing, presence, loc)

    private fun ev(kind: String, vararg kv: Pair<String, String>) =
        TripEventPayload("e", kind, null, JsonObject(kv.associate { it.first to JsonPrimitive(it.second) }))

    private fun live(u: String, s: String, seq: Long, ts: Long = 1000) = LiveLocation(u, s, seq, 10.0, 106.0, null, null, null, ts)

    @Test fun snapshotSeedsMembersAndCursor() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(7, 0, 8, member("a"), member("b", presence = "offline")))
        val s = m.state()
        assertEquals(7, s.headEventSeq)
        assertEquals(listOf("a", "b"), s.members.map { it.userId })
        assertTrue(s.members[0].online && !s.members[1].online)
        assertEquals("Đà Lạt", s.name)
    }

    @Test fun snapshotKeepsCursorBeforeReplayWindow() {
        // head 10, replay will deliver 8..10: the cursor must wait at 7 so the replayed events apply
        val m = TripLiveModel("T", lastEventSeq = 7)
        m.applySnapshot(snap(10, 3, 8, member("a")))
        assertEquals(7, m.cursor.lastSeq)
        assertEquals(EventOutcome.Applied, m.applyTripEvent(8, ev("trip_started")))
        assertEquals(EventOutcome.Duplicate, m.applyTripEvent(8, ev("trip_started")))
    }

    @Test fun locationNeverMovesBackwards() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(1, 0, 2, member("a")))
        m.applyLocations(listOf(live("a", "S2", 5)))
        m.applyLocations(listOf(live("a", "S2", 3))) // stale
        m.applyLocations(listOf(live("a", "S1", 99))) // older session (UUIDv7 order)
        assertEquals(5, m.state().members[0].location!!.seq)
        m.applyLocations(listOf(live("a", "S3", 1)))
        assertEquals("S3", m.state().members[0].location!!.sessionId)
    }

    @Test fun pausedSharingHidesAndDropsLocation() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(1, 0, 2, member("a")))
        m.applyLocations(listOf(live("a", "S", 1)))
        assertEquals(EventOutcome.Applied, m.applyTripEvent(2, ev("member_sharing_changed", "user_id" to "a", "sharing_state" to "paused")))
        assertNull(m.state().members[0].location)
        m.applyLocations(listOf(live("a", "S", 2))) // ignored while paused
        assertNull(m.state().members[0].location)
    }

    @Test fun gapAsksForResyncAndDoesNotApply() {
        val m = TripLiveModel("T", 5)
        val out = m.applyTripEvent(8, ev("trip_ended"))
        assertEquals(EventOutcome.Gap(6, 7), out)
        assertEquals(5, m.cursor.lastSeq)
        assertEquals("", m.state().status)
    }

    @Test fun membershipEventsAndOwnership() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(1, 0, 2, member("a")))
        m.applyTripEvent(2, ev("member_joined", "user_id" to "b", "display_name" to "Bình"))
        assertEquals(listOf("a", "b"), m.state().members.map { it.userId })
        m.applyTripEvent(3, ev("ownership_transferred", "from" to "a", "to" to "b"))
        assertEquals("owner", m.state().members.first { it.userId == "b" }.role)
        m.applyTripEvent(4, ev("tracking_started", "user_id" to "b", "session_id" to "x"))
        assertTrue(m.state().members.first { it.userId == "b" }.recording)
        m.applyTripEvent(5, ev("member_removed", "user_id" to "b"))
        assertEquals(listOf("a"), m.state().members.map { it.userId })
    }

    @Test fun sosSharesTheEventSequenceAndTracksDelivery() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(1, 0, 2, member("a")))
        assertEquals(EventOutcome.Applied, m.applySos(2, SosEventPayload("S1", "a", "A", "breakdown", "Hỏng xe", 1.0, 2.0)))
        assertEquals(EventOutcome.Duplicate, m.applySos(2, SosEventPayload("S1", "a", "A", "breakdown", null, null, null)))
        m.applySosStatus(SosStatusPayload("S1", total = 3, received = 2, seen = 1))
        assertEquals(2, m.state().sos[0].delivery!!.received)
        m.applyTripEvent(3, ev("sos_resolved", "sos_id" to "S1", "status" to "resolved"))
        assertEquals("resolved", m.state().sos[0].status)
    }

    @Test fun staleAndLostFollowServerTime() {
        val m = TripLiveModel("T")
        m.applySnapshot(snap(1, 0, 2, member("a")))
        m.applyLocations(listOf(live("a", "S", 1, ts = 100_000)))
        val a = m.state().members[0]
        assertEquals(MemberStatus.LIVE, m.statusOf(a, nowMs = 110_000, clockOffsetMs = 0))
        assertEquals(MemberStatus.STALE, m.statusOf(a, nowMs = 140_000, clockOffsetMs = 0))
        assertEquals(MemberStatus.LOST, m.statusOf(a, nowMs = 200_000, clockOffsetMs = 0))
        assertNotNull(a.location)
    }
}
