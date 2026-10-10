package com.motolink.core.net

import kotlinx.serialization.json.Json
import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

/**
 * Decodes the golden server responses (server/testdata/contract, produced by `go test -run TestGoldenContract`)
 * with the app's real DTOs. A renamed or retyped field on either side fails here instead of in the field.
 */
class ContractTest {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }

    private fun fixture(name: String): String {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            val f = File(dir, "server/testdata/contract/$name.json")
            if (f.exists()) return f.readText()
            dir = dir.parentFile
        }
        error("fixture $name not found above ${File("").absolutePath}")
    }

    private inline fun <reified T> read(name: String): T = json.decodeFromString(fixture(name))

    @Test fun tripCreateAndGet() {
        val c = read<TripEnvelope>("trip_create")
        assertEquals("Đà Lạt", c.trip.name)
        assertEquals("draft", c.trip.status)
        val g = read<TripDetailResponse>("trip_get")
        assertEquals("owner", g.myMembership.role)
        assertEquals(1, g.memberCount)
    }

    @Test fun invitationAndJoin() {
        assertTrue(read<InvitationDto>("invitation_create").code.length >= 6)
        val p = read<InvitePreview>("invitation_resolve")
        assertTrue(p.valid)
        assertEquals("Đà Lạt", p.tripName)
        val j = read<JoinResponse>("trip_join")
        assertEquals("member", j.myMembership.role)
        assertEquals("active", j.myMembership.status)
    }

    @Test fun membersAndPatch() {
        val m = read<MemberListResponse>("members")
        assertEquals(2, m.items.size)
        assertTrue(m.items.all { it.displayName.isNotEmpty() })
        assertEquals("paused", read<MembershipEnvelope>("member_patch").membership.sharingState)
    }

    @Test fun tripStart() {
        assertEquals("active", read<TripEnvelope>("trip_start").trip.status)
    }

    @Test fun sessions() {
        val s = read<SessionEnvelope>("session_start").session
        assertEquals("active", s.status)
        assertEquals("2026-01-01", s.sessionMonth)
        assertEquals("paused", read<SessionEnvelope>("session_pause").session.status)
    }

    @Test fun bootstrapAndSnapshot() {
        val b = read<BootstrapResponse>("bootstrap")
        assertEquals(1, b.trips.size)
        assertNotNull(b.trips[0].myOpenSession)
        assertEquals("active", b.trips[0].trip.status)
        assertTrue(read<SnapshotDto>("snapshot").headEventSeq > 0)
    }

    @Test fun pointsAck() {
        val p = read<PointsResponse>("points")
        assertEquals(2, p.upToSeq)
        assertEquals(listOf(2L), p.rejected.map { it.seq })
        assertEquals("invalid_coordinates", p.rejected[0].reason)
    }

    @Test fun sosAndNotifications() {
        val s = read<SosCreateResponse>("sos_create")
        assertTrue(s.eventSeq > 0)
        assertEquals(false, s.duplicate)
        val n = read<NotificationListResponse>("notifications")
        assertTrue(n.items.any { it.type == "sos" })
        assertTrue(n.items.all { it.readAt == null })
    }
}
