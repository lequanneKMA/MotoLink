package com.motolink.logic

enum class MotionState { MOVING, SLOW, STILL, STILL_LONG }

/** What the location source should request next. Mirrors docs/design/12 §4.2. */
data class SamplingPlan(
    val state: MotionState,
    val intervalMs: Long,
    val minDistanceM: Float,
    val highAccuracy: Boolean,
)

/**
 * Chooses the GPS request profile from smoothed speed. Hysteresis: speeding up is
 * recognised immediately, settling to "still" needs sustained low speed, so a red
 * light does not flip the profile.
 */
class AdaptiveSampler(
    private val movingMps: Float = 2.0f,
    private val stillMps: Float = 0.5f,
    private val stillAfterMs: Long = 20_000,
    private val stillLongAfterMs: Long = 120_000,
    private val emaAlpha: Float = 0.4f,
) {
    private var smoothed: Float? = null
    private var lowSinceMs: Long? = null
    var current: SamplingPlan = plan(MotionState.MOVING)
        private set

    /** Feed each accepted fix. Returns a new plan only if it changed, otherwise null. */
    fun onFix(tsMs: Long, speedMps: Float?): SamplingPlan? {
        val v = (speedMps ?: smoothed ?: 0f).coerceAtLeast(0f)
        val s = smoothed?.let { it + emaAlpha * (v - it) } ?: v
        smoothed = s

        val next: MotionState = when {
            s > movingMps -> {
                lowSinceMs = null
                MotionState.MOVING
            }
            s >= stillMps -> {
                lowSinceMs = null
                MotionState.SLOW
            }
            else -> {
                val since = lowSinceMs ?: tsMs.also { lowSinceMs = it }
                val low = tsMs - since
                when {
                    low >= stillLongAfterMs -> MotionState.STILL_LONG
                    low >= stillAfterMs -> MotionState.STILL
                    // not low for long enough yet: keep the previous profile
                    else -> current.state
                }
            }
        }
        if (next == current.state) return null
        current = plan(next)
        return current
    }

    fun reset() {
        smoothed = null
        lowSinceMs = null
        current = plan(MotionState.MOVING)
    }

    companion object {
        fun plan(state: MotionState): SamplingPlan = when (state) {
            MotionState.MOVING -> SamplingPlan(state, 3_000, 5f, true)
            MotionState.SLOW -> SamplingPlan(state, 5_000, 5f, true)
            MotionState.STILL -> SamplingPlan(state, 20_000, 0f, false)
            MotionState.STILL_LONG -> SamplingPlan(state, 30_000, 0f, false)
        }
    }
}

sealed interface FixDecision {
    data class Accept(val suspect: Boolean) : FixDecision
    data class Reject(val reason: String) : FixDecision
}

/** Client-side pre-filter. The server validates again (docs/design/07 §8); this only saves bandwidth. */
class QualityFilter(
    private val maxAccuracyM: Float = 50f,
    private val maxSpeedMps: Float = 70f,
    private val maxJumpMps: Double = 90.0,
) {
    private var last: Fix? = null

    fun evaluate(f: Fix): FixDecision {
        if (!Geo.validCoordinates(f.lat, f.lng)) return FixDecision.Reject("invalid_coordinates")
        val acc = f.accuracyM
        if (acc == null || acc <= 0f || acc > maxAccuracyM) return FixDecision.Reject("low_accuracy")
        val prev = last
        if (prev != null) {
            if (f.clientTsMs <= prev.clientTsMs) return FixDecision.Reject("out_of_order")
            val dt = (f.clientTsMs - prev.clientTsMs) / 1000.0
            if (Geo.distance(prev, f) / dt > maxJumpMps) return FixDecision.Reject("jump")
        }
        last = f
        val speed = f.speedMps
        return FixDecision.Accept(suspect = speed != null && speed > maxSpeedMps)
    }

    fun reset() {
        last = null
    }
}
