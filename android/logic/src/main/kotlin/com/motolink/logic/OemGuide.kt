package com.motolink.logic

/**
 * Per-manufacturer steps that reduce the chance the OS kills background recording
 * (docs/design/12 §5.3). They lower the probability, they cannot guarantee anything.
 *
 * IMPORTANT: menu names differ between ROM versions and are NOT verified
 * (docs/design/research/unverified.md U13). The M1 field test on real devices is the
 * source of truth; update this table from its results.
 */
data class OemGuide(val brand: String, val steps: List<String>) {
    companion object {
        private val common = listOf(
            "Cài đặt > Ứng dụng > MotoLink > Pin: chọn \"Không giới hạn\" (hoặc \"Không tối ưu hóa\").",
            "Giữ thông báo \"Đang ghi hành trình\" luôn bật; không vuốt tắt ứng dụng khỏi danh sách ứng dụng gần đây khi đang ghi.",
        )

        fun forManufacturer(manufacturer: String?): OemGuide {
            val m = manufacturer.orEmpty().trim().lowercase()
            return when {
                m in setOf("xiaomi", "redmi", "poco") -> OemGuide(
                    "Xiaomi / Redmi / POCO",
                    listOf(
                        "Cài đặt > Ứng dụng > Quản lý ứng dụng > MotoLink > Tự khởi động: bật.",
                        "Cài đặt > Ứng dụng > Quản lý ứng dụng > MotoLink > Tiết kiệm pin: chọn \"Không hạn chế\".",
                        "Mở danh sách ứng dụng gần đây, nhấn giữ thẻ MotoLink và chọn biểu tượng khóa.",
                    ) + common,
                )
                m in setOf("oppo", "realme", "oneplus") -> OemGuide(
                    "OPPO / realme / OnePlus",
                    listOf(
                        "Cài đặt > Pin > MotoLink: bật \"Cho phép hoạt động nền\" và tắt tối ưu hóa pin.",
                        "Cài đặt > Quản lý ứng dụng > MotoLink > Cho phép tự khởi động: bật.",
                        "Khóa MotoLink trong danh sách ứng dụng gần đây.",
                    ) + common,
                )
                m == "vivo" || m == "iqoo" -> OemGuide(
                    "vivo / iQOO",
                    listOf(
                        "Cài đặt > Pin > Tiêu thụ pin nền cao: cho phép MotoLink.",
                        "i Quản lý > Quản lý ứng dụng > Tự khởi động: bật cho MotoLink.",
                        "Khóa MotoLink trong danh sách ứng dụng gần đây.",
                    ) + common,
                )
                m == "samsung" -> OemGuide(
                    "Samsung",
                    listOf(
                        "Cài đặt > Chăm sóc thiết bị > Pin > Giới hạn sử dụng nền: bỏ MotoLink khỏi danh sách \"Ứng dụng ngủ\" và \"Ứng dụng ngủ sâu\".",
                        "Cài đặt > Ứng dụng > MotoLink > Pin: chọn \"Không giới hạn\".",
                    ) + common,
                )
                m == "huawei" || m == "honor" -> OemGuide(
                    "Huawei / Honor",
                    listOf(
                        "Cài đặt > Pin > Khởi chạy ứng dụng > MotoLink: chọn \"Quản lý thủ công\" và bật cả ba tùy chọn.",
                    ) + common,
                )
                else -> OemGuide("Thiết bị của bạn", common)
            }
        }
    }
}
