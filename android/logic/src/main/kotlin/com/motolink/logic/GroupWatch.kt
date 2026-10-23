package com.motolink.logic

/** The rider the group should look after first (docs: "Người bị tụt lại"). */
data class Laggard(val member: MemberLive, val status: MemberStatus, val distanceM: Double?, val silentS: Long?)

/** Pure decisions for the ride screen, so they are unit-tested instead of buried in composables. */
object GroupWatch {
    const val FAR_M = 1_000.0

    /** Age in seconds of a member's last position, in server time; null when there is none. */
    fun ageS(m: MemberLive, nowMs: Long, clockOffsetMs: Long): Long? =
        m.location?.let { ((nowMs + clockOffsetMs) - it.serverTs).coerceAtLeast(0) / 1000 }

    fun statusOf(m: MemberLive, nowMs: Long, clockOffsetMs: Long): MemberStatus =
        PresenceClassifier.classify(m.online, m.sharing, m.location?.serverTs, nowMs, clockOffsetMs)

    /**
     * Someone who lost signal (position older than 90 s) or is more than [farM] away from [me]. Lost signal wins over
     * distance; among equals the farthest wins. Members who hide their location or have never sent one are not flagged.
     */
    fun laggard(
        members: List<MemberLive>, meId: String?, myLat: Double?, myLng: Double?,
        nowMs: Long, clockOffsetMs: Long, farM: Double = FAR_M,
    ): Laggard? {
        val out = members.asSequence()
            .filter { it.userId != meId && it.sharing && it.location != null }
            .map { m ->
                val st = statusOf(m, nowMs, clockOffsetMs)
                val d = if (myLat != null && myLng != null) Geo.haversineMeters(myLat, myLng, m.location!!.lat, m.location.lng) else null
                Laggard(m, st, d, ageS(m, nowMs, clockOffsetMs))
            }
            .filter { it.status == MemberStatus.LOST || (it.distanceM != null && it.distanceM > farM) }
            .sortedWith(compareByDescending<Laggard> { it.status == MemberStatus.LOST }.thenByDescending { it.distanceM ?: 0.0 })
            .firstOrNull()
        return out
    }

    /** One line for the banner: "Hùng cách 1,2 km · mất tín hiệu 2 phút". */
    fun bannerText(l: Laggard): String {
        val parts = ArrayList<String>(2)
        l.distanceM?.let { parts += "cách ${RideText.distance(it)}" }
        if (l.status == MemberStatus.LOST) parts += "mất tín hiệu" + (l.silentS?.let { " ${RideText.silence(it)}" } ?: "")
        return l.member.name + " " + parts.joinToString(" · ")
    }

    /** Second line of a member row. */
    fun rowText(m: MemberLive, nowMs: Long, clockOffsetMs: Long): String {
        val st = statusOf(m, nowMs, clockOffsetMs)
        val age = ageS(m, nowMs, clockOffsetMs)
        val kmh = m.location?.speedMps?.let { Math.round(it * 3.6).toInt() }
        return when (st) {
            MemberStatus.SHARING_HIDDEN -> "Đã tắt chia sẻ vị trí"
            MemberStatus.OFFLINE -> "Ngoại tuyến"
            MemberStatus.LIVE -> "Trực tiếp" + (kmh?.let { " · $it km/h" } ?: "")
            MemberStatus.STALE -> "${RideText.silence(age ?: 0)} trước"
            MemberStatus.LOST -> "Mất tín hiệu ${RideText.silence(age ?: 0)}"
        }
    }

    /** Members who are online and sharing, for "5/6 người". */
    fun onlineCount(members: List<MemberLive>): Int = members.count { it.online }
}
