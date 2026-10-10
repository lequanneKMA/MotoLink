package com.motolink.core.common

/** Injectable clock so time-dependent code can be tested. */
fun interface Clock {
    fun nowMs(): Long

    companion object {
        val System = Clock { java.lang.System.currentTimeMillis() }
    }
}
