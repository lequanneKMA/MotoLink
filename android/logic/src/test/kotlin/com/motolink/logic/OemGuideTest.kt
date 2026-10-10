package com.motolink.logic

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class OemGuideTest {
    @Test fun matchesCommonBrandsCaseInsensitively() {
        assertTrue(OemGuide.forManufacturer("Xiaomi").brand.startsWith("Xiaomi"))
        assertTrue(OemGuide.forManufacturer("REDMI").brand.startsWith("Xiaomi"))
        assertTrue(OemGuide.forManufacturer("samsung").brand == "Samsung")
        assertTrue(OemGuide.forManufacturer("realme").brand.startsWith("OPPO"))
        assertTrue(OemGuide.forManufacturer("HONOR").brand.startsWith("Huawei"))
    }

    @Test fun unknownOrMissingFallsBackToGenericSteps() {
        assertEquals("Thiết bị của bạn", OemGuide.forManufacturer("Nokia").brand)
        assertEquals("Thiết bị của bạn", OemGuide.forManufacturer(null).brand)
        assertTrue(OemGuide.forManufacturer(null).steps.isNotEmpty())
    }

    @Test fun everyGuideEndsWithTheCommonBatteryStep() {
        for (b in listOf("xiaomi", "oppo", "vivo", "samsung", "huawei", "other")) {
            assertTrue(OemGuide.forManufacturer(b).steps.any { it.contains("Pin") }, b)
        }
    }
}
