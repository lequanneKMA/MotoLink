package com.motolink.logic

import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class JwtTest {
    private fun jwt(payload: String) =
        listOf("""{"alg":"EdDSA"}""", payload, "sig").joinToString(".") { Base64.getUrlEncoder().withoutPadding().encodeToString(it.toByteArray()) }

    @Test fun readsExpInMillis() {
        assertEquals(1_760_000_000_000, JwtExpiry.expiresAtMs(jwt("""{"sub":"u","exp":1760000000,"sid":"s"}""")))
        assertEquals(5_000, JwtExpiry.expiresAtMs(jwt("""{ "exp" : 5 }""")))
    }

    @Test fun readsSubject() {
        assertEquals("u-1", JwtExpiry.subject(jwt("""{"sub":"u-1","exp":1}""")))
        assertNull(JwtExpiry.subject("x"))
    }

    @Test fun garbageYieldsNull() {
        assertNull(JwtExpiry.expiresAtMs("not-a-jwt"))
        assertNull(JwtExpiry.expiresAtMs(jwt("""{"sub":"u"}""")))
        assertNull(JwtExpiry.expiresAtMs("a.%%%.c"))
    }
}
