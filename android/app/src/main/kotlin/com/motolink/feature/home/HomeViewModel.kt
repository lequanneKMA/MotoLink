package com.motolink.feature.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.motolink.core.db.LocalSessionEntity
import com.motolink.feature.tracking.TrackingRepository
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.toFix
import com.motolink.logic.RouteMath
import com.motolink.logic.RouteStats
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import org.maplibre.android.geometry.LatLng

enum class RecState { IDLE, ACTIVE, PAUSED, INTERRUPTED }

data class HomeUi(
    val state: RecState = RecState.IDLE,
    val session: LocalSessionEntity? = null,
    val route: List<LatLng> = emptyList(),
    val stats: RouteStats = RouteStats(0.0, 0, 0.0, 0),
    val pending: Int = 0,
    val gpsAvailable: Boolean = true,
    val speedKmh: Int? = null,
)

@OptIn(ExperimentalCoroutinesApi::class)
class HomeViewModel(private val repo: TrackingRepository) : ViewModel() {

    private val pointsOfOpen: Flow<List<com.motolink.core.db.TrackPointEntity>> =
        repo.openSession.flatMapLatest { s -> if (s == null) flowOf(emptyList()) else repo.points(s.sessionId) }

    val ui: StateFlow<HomeUi> = combine(
        repo.openSession, pointsOfOpen, repo.pendingCount, TrackingService.running, TrackingService.gpsAvailable,
    ) { session, points, pending, running, gps ->
        val fixes = points.filter { !it.suspect }.map { it.toFix() }
        HomeUi(
            state = when {
                session == null -> RecState.IDLE
                !running -> RecState.INTERRUPTED // open in Room but the service is gone: killed by the OS or the process died
                session.status == "paused" -> RecState.PAUSED
                else -> RecState.ACTIVE
            },
            session = session,
            route = fixes.map { LatLng(it.lat, it.lng) },
            stats = RouteMath.stats(fixes),
            pending = pending,
            gpsAvailable = gps,
            speedKmh = points.lastOrNull()?.speedMps?.let { Math.round(it * 3.6).toInt() },
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), HomeUi())

    /** Creates the session row. The caller starts the foreground service afterwards (user-initiated). */
    fun prepareSession(then: () -> Unit) {
        viewModelScope.launch {
            repo.startSolo()
            then()
        }
    }
}
