package com.motolink.feature.nav

import com.motolink.core.net.ApiResult
import com.motolink.core.net.ApiService
import com.motolink.core.net.LatLngDto
import com.motolink.core.net.PlaceDto
import com.motolink.core.net.RouteDto
import com.motolink.core.net.RouteRequest
import com.motolink.core.net.apiCall
import com.motolink.logic.GeoPoint
import com.motolink.logic.NavRoute
import com.motolink.logic.NavStep
import com.motolink.logic.Polyline

/** A route the rider can choose, with the texts the list shows. */
class NavOption(val label: String, val route: NavRoute, val distanceM: Double, val durationS: Double)

/** Routing and place search through the app's own server (which talks to OSRM and the PostGIS place table). */
class NavRepository(private val api: ApiService) {

    suspend fun routes(from: GeoPoint, to: GeoPoint): ApiResult<List<NavOption>> =
        when (val r = apiCall { api.route(RouteRequest(LatLngDto(from.lat, from.lng), LatLngDto(to.lat, to.lng), alternatives = true)) }) {
            is ApiResult.Err -> r
            is ApiResult.Ok -> {
                val best = r.value.routes.minOfOrNull { it.durationS } ?: 0.0
                ApiResult.Ok(
                    r.value.routes.mapIndexedNotNull { i, rt ->
                        runCatching { option(i, rt, best) }.getOrNull() // a route with a broken geometry is skipped, not fatal
                    },
                )
            }
        }

    private fun option(index: Int, rt: RouteDto, bestS: Double): NavOption {
        val pts = Polyline.decode(rt.geometry, 6)
        require(pts.size >= 2) { "empty geometry" }
        val steps = rt.steps.map { NavStep(it.distanceM, it.durationS, it.name, it.type, it.modifier, it.exit, GeoPoint(it.at.lat, it.at.lng)) }
        val label = when {
            index == 0 -> "Nhanh nhất"
            else -> "Đường khác" + (Math.round((rt.durationS - bestS) / 60.0).takeIf { it > 0 }?.let { " · +$it phút" } ?: "")
        }
        return NavOption(label, NavRoute(pts, steps, rt.durationS), rt.distanceM, rt.durationS)
    }

    suspend fun search(q: String, near: GeoPoint?): ApiResult<List<PlaceDto>> =
        apiCall { api.geocode(q, near?.lat, near?.lng).items }
}
