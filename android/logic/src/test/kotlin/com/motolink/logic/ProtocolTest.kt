package com.motolink.logic

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** Golden frames are copied from docs/design/07-websocket-protocol.md. */
class ProtocolTest {
    private fun parse(s: String): JsonObject = Json.parseToJsonElement(s).jsonObject

    @Test fun encodeHelloMatchesContract() {
        val f = ProtocolCodec.hello(
            HelloPayload("dev-1", "0.1.0", "android-14", 1760072400000, resume = listOf(ResumeEntry("T", 41))), id = "c1",
        )
        val o = parse(f)
        assertEquals("1", o["v"]?.jsonPrimitive?.content, "v is mandatory on every frame: $f")
        assertEquals("hello", o["type"]!!.jsonPrimitive.content)
        val p = o["payload"]!!.jsonObject
        assertEquals("1760072400000", p["client_time_ms"]!!.jsonPrimitive.content)
        assertEquals("dev-1", p["device_id"]!!.jsonPrimitive.content)
        assertEquals("41", p["resume"]!!.toString().let { Regex("\"last_event_seq\":(\\d+)").find(it)!!.groupValues[1] })
        assertEquals("[1]", p["supported_v"].toString(), "supported_v must be sent even when it equals the default")
    }

    @Test fun encodeLocationUpdateOmitsNullFields() {
        val f = ProtocolCodec.locationUpdate(
            "T", LocationUpdatePayload("S1", listOf(WirePoint(813, 1760072402890, 11.94052, 108.45841, accuracyM = 7.5f))), 1760072403000,
        )
        val o = parse(f)
        assertEquals("T", o["trip_id"]!!.jsonPrimitive.content)
        val pt = o["payload"]!!.jsonObject["points"].toString()
        assertTrue("\"seq\":813" in pt && "\"accuracy_m\":7.5" in pt, pt)
        assertFalse("speed_mps" in pt, "null optional fields are not sent: $pt")
    }

    @Test fun sosCreateUsesClientSosIdAsMessageId() {
        val f = ProtocolCodec.sosCreate("T", SosCreatePayload("7d4f", "breakdown", "Hỏng xe", 11.94, 108.45, 12f, 1L), 2L)
        val o = parse(f)
        assertEquals("7d4f", o["id"]!!.jsonPrimitive.content)
        assertEquals("sos.create", o["type"]!!.jsonPrimitive.content)
        assertTrue("Hỏng xe" in f, "UTF-8 text must round-trip")
    }

    @Test fun decodeWelcome() {
        val m = ProtocolCodec.decode(
            """{"v":1,"type":"welcome","ts":1760072400050,"payload":{"conn_id":"c","user_id":"u","server_time_ms":1760072400050,
            "clock_offset_ms":50,"v_min":1,"v_max":1,"heartbeat":{"server_ping_interval_s":20,"pong_timeout_s":10},
            "limits":{"max_frame_bytes":16384,"max_points_per_frame":20}}}""",
        )
        assertIs<Inbound.Welcome>(m)
        assertEquals(50, m.p.clockOffsetMs)
        assertEquals(20, m.p.limits.maxPointsPerFrame)
    }

    @Test fun decodeTripJoinedSnapshot() {
        val m = ProtocolCodec.decode(
            """{"v":1,"type":"trip.joined","trip_id":"T","ts":1760072400100,"payload":{
              "trip":{"status":"active","name":"Đà Lạt 3N2Đ"},"head_event_seq":45,
              "replay":{"from_seq":42,"count":4,"complete":true},
              "members":[
               {"user_id":"U1","display_name":"Minh","color":"#E53935","role":"owner","sharing_state":"sharing","presence":"online",
                "location":{"session_id":"S1","seq":812,"lat":11.9404,"lng":108.4583,"accuracy_m":8.0,"speed_mps":9.2,"heading_deg":135,"server_ts":1760072399000}},
               {"user_id":"U2","display_name":"Lan","color":"#1E88E5","role":"member","sharing_state":"paused","presence":"online","location":null}]}}""",
        )
        assertIs<Inbound.TripJoined>(m)
        assertEquals(45, m.p.headEventSeq)
        assertEquals(PositionKey("S1", 812), m.p.members[0].location!!.key)
        assertNull(m.p.members[1].location, "paused member has no location")
        assertEquals("Đà Lạt 3N2Đ", m.p.trip.name)
    }

    @Test fun decodeLocationEventAndAck() {
        val e = ProtocolCodec.decode(
            """{"v":1,"type":"location.event","trip_id":"T","payload":{"points":[{"user_id":"U1","session_id":"S1","seq":813,"lat":11.94052,"lng":108.45841,"server_ts":5}]}}""",
        )
        assertIs<Inbound.LocationEvent>(e)
        assertEquals(PositionKey("S1", 813), e.p.points.single().key)
        val a = ProtocolCodec.decode(
            """{"v":1,"type":"location.ack","trip_id":"T","payload":{"session_id":"S1","up_to_seq":820,"rejected":[{"seq":815,"reason":"low_accuracy"}]}}""",
        )
        assertIs<Inbound.LocationAck>(a)
        assertEquals(820, a.p.upToSeq)
        assertEquals("low_accuracy", a.p.rejected.single().reason)
    }

    @Test fun decodeDurableEventsCarryEventSeq() {
        val t = ProtocolCodec.decode(
            """{"v":1,"type":"trip.event","trip_id":"T","event_seq":46,"payload":{"event_id":"E46","kind":"member_joined","actor_id":"U3","data":{"user_id":"U3","display_name":"Hà"}}}""",
        )
        assertIs<Inbound.TripEvent>(t)
        assertEquals(46, t.eventSeq)
        assertEquals("member_joined", t.p.kind)
        val s = ProtocolCodec.decode(
            """{"v":1,"type":"sos.event","trip_id":"T","event_seq":47,"payload":{"sos_id":"S-9","sender_id":"U1","sender_name":"Minh","kind":"breakdown","message":"Hỏng xe","lat":11.94,"lng":108.45,"accuracy_m":12,"created_at":1}}""",
        )
        assertIs<Inbound.SosEvent>(s)
        assertEquals("S-9", s.p.sosId)
        // durable events without event_seq are malformed
        assertIs<Inbound.Malformed>(ProtocolCodec.decode("""{"v":1,"type":"trip.event","trip_id":"T","payload":{"event_id":"x","kind":"k"}}"""))
    }

    @Test fun decodeErrorsAckAndControl() {
        val e = ProtocolCodec.decode("""{"v":1,"type":"error","payload":{"ref":"c2","code":"forbidden","message":"x","retryable":false}}""")
        assertIs<Inbound.Error>(e); assertEquals("forbidden", e.p.code)
        val r = ProtocolCodec.decode("""{"v":1,"type":"error","payload":{"code":"rate_limited","retry_after_ms":1500}}""")
        assertIs<Inbound.Error>(r); assertEquals(1500, r.p.retryAfterMs)
        val a = ProtocolCodec.decode("""{"v":1,"type":"ack","payload":{"ref":"7d4f","ok":true,"result":{"sos_id":"S-9","event_seq":47,"duplicate":false}}}""")
        assertIs<Inbound.Ack>(a); assertEquals("7d4f", a.p.ref)
        assertIs<Inbound.ResyncRequired>(ProtocolCodec.decode("""{"v":1,"type":"resync.required","trip_id":"T","payload":{"reason":"gap_too_large"}}"""))
        val c = ProtocolCodec.decode("""{"v":1,"type":"closing","payload":{"reason":"server_shutdown","retry_after_ms":3000}}""")
        assertIs<Inbound.Closing>(c); assertEquals(3000, c.p.retryAfterMs)
        val p = ProtocolCodec.decode("""{"v":1,"type":"pong","payload":{"ref":"p1","server_time_ms":1}}""")
        assertIs<Inbound.Pong>(p); assertEquals("p1", p.ref)
        val pr = ProtocolCodec.decode("""{"v":1,"type":"presence.event","trip_id":"T","payload":{"user_id":"U2","state":"offline","at":1760072410000}}""")
        assertIs<Inbound.Presence>(pr); assertEquals("offline", pr.p.state)
    }

    @Test fun forwardCompatibility() {
        // unknown type: ignored, not an error
        assertEquals(Inbound.Unknown("future.thing"), ProtocolCodec.decode("""{"v":2,"type":"future.thing","payload":{"x":1}}"""))
        // unknown extra fields in known payloads are ignored
        val w = ProtocolCodec.decode("""{"v":1,"type":"welcome","payload":{"conn_id":"c","user_id":"u","server_time_ms":1,"brand_new_field":[1,2]}}""")
        assertIs<Inbound.Welcome>(w)
        // unknown trip.event kind still decodes (the cursor must advance)
        val k = ProtocolCodec.decode("""{"v":1,"type":"trip.event","trip_id":"T","event_seq":9,"payload":{"event_id":"E","kind":"from_the_future"}}""")
        assertIs<Inbound.TripEvent>(k)
    }

    @Test fun malformedInputNeverThrows() {
        for (bad in listOf("", "not json", "{}", """{"type":1}""", """{"v":1,"type":"welcome"}""", """{"v":1,"type":"trip.joined","payload":{}}""", "[]")) {
            assertIs<Inbound.Malformed>(ProtocolCodec.decode(bad), "input: $bad")
        }
    }
}
