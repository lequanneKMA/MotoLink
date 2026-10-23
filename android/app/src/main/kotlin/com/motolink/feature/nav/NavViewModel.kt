package com.motolink.feature.nav

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.motolink.core.location.LocationSource
import com.motolink.core.location.LocationSources
import com.motolink.core.net.ApiResult
import com.motolink.core.net.PlaceDto
import com.motolink.core.net.userMessage
import com.motolink.logic.Fix
import com.motolink.logic.GeoPoint
import com.motolink.logic.MotionState
import com.motolink.logic.NavState
import com.motolink.logic.NavTracker
import com.motolink.logic.NavText
import com.motolink.logic.SamplingPlan
import com.motolink.MotoLinkApp
import com.motolink.core.location.OneShotLocation
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class NavStage { SEARCH, PICK, NAVIGATING, ARRIVED }

data class NavUi(
    val stage: NavStage = NavStage.SEARCH,
    val query: String = "",
    val results: List<PlaceDto> = emptyList(),
    val searching: Boolean = false,
    val dest: PlaceDto? = null,
    val options: List<NavOption> = emptyList(),
    val selected: Int = 0,
    val loading: Boolean = false,
    val me: GeoPoint? = null,
    val bearing: Float? = null,
    val speedKmh: Int? = null,
    val nav: NavState? = null,
    val rerouting: Boolean = false,
    val voice: Boolean = true,
    val message: String? = null,
)

/**
 * Search → choose a route → follow it. Positions come from the phone's location provider while this screen is open
 * (foreground navigation); the route comes from the server, the tracking along it is done by [NavTracker].
 */
class NavViewModel(app: Application) : AndroidViewModel(app) {
    private val c get() = getApplication<MotoLinkApp>().container
    private val _ui = MutableStateFlow(NavUi())
    val ui: StateFlow<NavUi> = _ui.asStateFlow()

    private val speech = Speech(app)
    private var source: LocationSource? = null
    private var tracker: NavTracker? = null
    private var searchJob: Job? = null
    private var lastReroute = 0L

    // ---- search ----
    fun setQuery(q: String) {
        _ui.update { it.copy(query = q) }
        searchJob?.cancel()
        if (q.trim().length < 2) { _ui.update { it.copy(results = emptyList(), searching = false) }; return }
        searchJob = viewModelScope.launch {
            delay(300) // debounce typing
            _ui.update { it.copy(searching = true) }
            val near = _ui.value.me
            when (val r = c.nav.search(q, near)) {
                is ApiResult.Ok -> _ui.update { it.copy(results = r.value, searching = false) }
                is ApiResult.Err -> _ui.update { it.copy(results = emptyList(), searching = false, message = r.failure.userMessage()) }
            }
        }
    }

    /** Remember where the rider is (also biases search results towards them). */
    fun locate() {
        viewModelScope.launch {
            OneShotLocation.get(getApplication(), 5_000)?.let { f -> _ui.update { it.copy(me = GeoPoint(f.lat, f.lng)) } }
        }
    }

    fun pick(place: PlaceDto) {
        _ui.update { it.copy(dest = place, loading = true, stage = NavStage.PICK, results = emptyList(), options = emptyList(), selected = 0) }
        viewModelScope.launch {
            val from = _ui.value.me ?: OneShotLocation.get(getApplication(), 6_000)?.let { GeoPoint(it.lat, it.lng) }
            if (from == null) {
                _ui.update { it.copy(loading = false, stage = NavStage.SEARCH, message = "Chưa xác định được vị trí của bạn. Bật GPS và cấp quyền vị trí.") }
                return@launch
            }
            when (val r = c.nav.routes(from, GeoPoint(place.lat, place.lng))) {
                is ApiResult.Ok ->
                    if (r.value.isEmpty()) _ui.update { it.copy(loading = false, stage = NavStage.SEARCH, message = "Không tìm thấy đường đi.") }
                    else _ui.update { it.copy(loading = false, me = from, options = r.value) }
                is ApiResult.Err -> _ui.update { it.copy(loading = false, stage = NavStage.SEARCH, message = r.failure.userMessage()) }
            }
        }
    }

    fun select(i: Int) = _ui.update { it.copy(selected = i.coerceIn(0, (it.options.size - 1).coerceAtLeast(0))) }

    fun backToSearch() {
        stopLocation()
        _ui.update { it.copy(stage = NavStage.SEARCH, options = emptyList(), dest = null, nav = null) }
    }

    // ---- navigation ----
    fun start() {
        val opt = _ui.value.options.getOrNull(_ui.value.selected) ?: return
        tracker = NavTracker(opt.route)
        _ui.update { it.copy(stage = NavStage.NAVIGATING, nav = null, rerouting = false) }
        speech.enabled = _ui.value.voice
        speech.say("Bắt đầu dẫn đường. " + NavText.instruction(opt.route.steps.getOrNull(1) ?: opt.route.steps.first()).replaceFirstChar { it.lowercase() })
        startLocation()
    }

    fun toggleVoice() {
        val on = !_ui.value.voice
        speech.enabled = on
        _ui.update { it.copy(voice = on) }
    }

    fun end() {
        stopLocation()
        tracker = null
        _ui.update { it.copy(stage = NavStage.SEARCH, options = emptyList(), dest = null, nav = null, query = "", results = emptyList()) }
    }

    fun messageShown() = _ui.update { it.copy(message = null) }

    private fun startLocation() {
        stopLocation()
        source = LocationSources.create(getApplication()).also {
            it.start(SamplingPlan(MotionState.MOVING, 1_000, 0f, true), object : LocationSource.Listener {
                override fun onFix(fix: Fix) = onPosition(fix)
                override fun onAvailabilityChanged(available: Boolean) {
                    if (!available) _ui.update { s -> s.copy(message = "Mất tín hiệu GPS") }
                }
            })
        }
    }

    private fun stopLocation() {
        source?.stop()
        source = null
    }

    private fun onPosition(f: Fix) {
        val t = tracker ?: return
        val st = t.update(f.lat, f.lng)
        val me = GeoPoint(f.lat, f.lng)
        _ui.update { it.copy(me = me, bearing = f.headingDeg ?: it.bearing, speedKmh = f.speedMps?.let { v -> Math.round(v * 3.6).toInt() }, nav = st) }
        if (st.arrived) {
            speech.say("Bạn đã đến nơi")
            stopLocation()
            _ui.update { it.copy(stage = NavStage.ARRIVED) }
            return
        }
        t.prompt(st)?.let { speech.say(it) }
        if (st.offRoute) reroute(me)
    }

    /** Off the route for a few fixes: ask for a new route from here (at most every 10 s). */
    private fun reroute(from: GeoPoint) {
        val dest = _ui.value.dest ?: return
        val now = System.currentTimeMillis()
        if (_ui.value.rerouting || now - lastReroute < 10_000) return
        lastReroute = now
        _ui.update { it.copy(rerouting = true) }
        speech.say("Đang tìm đường mới")
        viewModelScope.launch {
            when (val r = c.nav.routes(from, GeoPoint(dest.lat, dest.lng))) {
                is ApiResult.Ok -> r.value.firstOrNull()?.let { best ->
                    tracker = NavTracker(best.route)
                    _ui.update { it.copy(options = listOf(best), selected = 0, rerouting = false, nav = null) }
                } ?: _ui.update { it.copy(rerouting = false) }
                is ApiResult.Err -> _ui.update { it.copy(rerouting = false, message = r.failure.userMessage()) }
            }
        }
    }

    override fun onCleared() {
        stopLocation()
        speech.shutdown()
    }
}
