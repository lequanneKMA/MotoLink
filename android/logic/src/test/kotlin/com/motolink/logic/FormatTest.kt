package com.motolink.logic

import java.util.Locale
import kotlin.test.Test
import kotlin.test.assertEquals

class FormatTest {
    @Test fun distance() {
        assertEquals("0 m", Format.distance(0.0))
        assertEquals("999 m", Format.distance(999.9))
        assertEquals("1.00 km", Format.distance(1000.0))
        assertEquals("12.35 km", Format.distance(12_345.0))
        assertEquals("0 m", Format.distance(-5.0))
    }

    @Test fun durationAndSpeed() {
        assertEquals("0:00", Format.duration(0))
        assertEquals("1:05", Format.duration(65))
        assertEquals("1:01:01", Format.duration(3661))
        assertEquals("36.0 km/h", Format.speedKmh(10.0))
    }

    @Test fun independentOfDefaultLocale() { // Vietnamese uses a decimal comma
        val old = Locale.getDefault()
        try {
            Locale.setDefault(Locale.forLanguageTag("vi-VN"))
            assertEquals("1.50 km", Format.distance(1500.0))
        } finally {
            Locale.setDefault(old)
        }
    }
}
