package com.motolink.logic

/** Reads the `exp` claim of a JWT without verifying it (the server verifies; this only schedules refreshes). */
object JwtExpiry {
    private val exp = Regex("\"exp\"\\s*:\\s*(\\d+)")

    fun expiresAtMs(jwt: String): Long? = runCatching {
        val payload = jwt.split('.')[1]
        val json = String(java.util.Base64.getUrlDecoder().decode(payload), Charsets.UTF_8)
        exp.find(json)?.groupValues?.get(1)?.toLong()?.times(1000)
    }.getOrNull()

    private val sub = Regex("\"sub\"\\s*:\\s*\"([^\"]+)\"")

    /** The `sub` claim (user id) of a JWT, unverified; for display logic only ("is this row me?"). */
    fun subject(jwt: String): String? = runCatching {
        val payload = jwt.split('.')[1]
        sub.find(String(java.util.Base64.getUrlDecoder().decode(payload), Charsets.UTF_8))?.groupValues?.get(1)
    }.getOrNull()
}
