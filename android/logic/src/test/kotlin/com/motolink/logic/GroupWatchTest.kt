package com.motolink.logic

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class GroupWatchTest {
    private fun loc(lat: Double, lng: Double, ts: Long, kmh: Double? = null) =
        LiveLocation("u", "S", 1, lat, lng, null, kmh?.let { (it / 3.6).toFloat() }, null, ts)

    private fun m(id: String, name: String, at: LiveLocation?, online: Boolean = true, sharing: Boolean = true) =
        MemberLive(id, name, null, "member", sharing, online, false, at)

    private val now = 1_000_000L

    @Test fun lostSignalBeatsDistance() {
        val far = m("a", "An", loc(10.0, 106.1, now - 1_000))                    // ~11 km away but live
        val lost = m("h", "Hùng", loc(10.0, 106.0001, now - 130_000))            // right here but silent 130 s
        val l = GroupWatch.laggard(listOf(far, lost), "me", 10.0, 106.0, now, 0)!!
        assertEquals("h", l.member.userId)
        assertEquals(MemberStatus.LOST, l.status)
        assertEquals(130, l.silentS)
    }

    @Test fun farLiveMemberIsFlaggedByDistance() {
        val l = GroupWatch.laggard(listOf(m("a", "An", loc(10.0, 106.02, now - 2_000))), "me", 10.0, 106.0, now, 0)!!
        assertEquals(MemberStatus.LIVE, l.status)
        assertTrue(l.distanceM!! > 2_000)
    }

    @Test fun nobodyToFlagWhenGroupIsTogether() {
        val group = listOf(m("a", "An", loc(10.0, 106.001, now - 3_000)), m("me", "Bạn", loc(10.0, 106.0, now)))
        assertNull(GroupWatch.laggard(group, "me", 10.0, 106.0, now, 0))
    }

    @Test fun hiddenOrNeverSeenMembersAreNotFlagged() {
        val group = listOf(m("a", "An", loc(10.0, 107.0, now - 500_000), sharing = false), m("b", "Bình", null))
        assertNull(GroupWatch.laggard(group, "me", 10.0, 106.0, now, 0))
    }

    @Test fun worksWithoutMyOwnPositionForLostSignal() {
        val l = GroupWatch.laggard(listOf(m("h", "Hùng", loc(10.0, 106.0, now - 100_000))), "me", null, null, now, 0)!!
        assertNull(l.distanceM)
        assertEquals("Hùng mất tín hiệu 1 phút", GroupWatch.bannerText(l))
    }

    @Test fun bannerAndRowWording() {
        val l = Laggard(m("h", "Hùng", loc(0.0, 0.0, 0)), MemberStatus.LOST, 1_200.0, 120)
        assertEquals("Hùng cách 1,2 km · mất tín hiệu 2 phút", GroupWatch.bannerText(l))
        val live = m("l", "Linh", loc(10.0, 106.0, now - 2_000, kmh = 49.0))
        assertEquals("Trực tiếp · 49 km/h", GroupWatch.rowText(live, now, 0))
        assertEquals("Ngoại tuyến", GroupWatch.rowText(m("t", "Tuấn", loc(10.0, 106.0, now), online = false), now, 0))
        assertEquals("40 giây trước", GroupWatch.rowText(m("m", "Minh", loc(10.0, 106.0, now - 40_000)), now, 0))
        assertEquals("Mất tín hiệu 2 phút", GroupWatch.rowText(m("h", "Hùng", loc(10.0, 106.0, now - 120_000)), now, 0))
    }

    @Test fun distanceWording() {
        assertEquals("850 m", RideText.distance(850.0))
        assertEquals("42 m", RideText.distance(42.0))
        assertEquals("1,2 km", RideText.distance(1_200.0))
        assertEquals("12 km", RideText.distance(12_400.0))
        assertEquals("30 giây", RideText.silence(30))
        assertEquals("1 giờ 5 phút", RideText.silence(3_900))
    }
}
