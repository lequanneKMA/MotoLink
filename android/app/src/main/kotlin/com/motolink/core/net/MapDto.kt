package com.motolink.core.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Shapes of GET /v1/config, POST /v1/route and GET /v1/geocode (server/internal/maps). */
@Serializable
data class ConfigDto(
    @SerialName("tile_style_url") val tileStyleUrl: String = "",
    @SerialName("routing_enabled") val routingEnabled: Boolean = false,
    @SerialName("search_enabled") val searchEnabled: Boolean = false,
)

@Serializable data class LatLngDto(val lat: Double, val lng: Double)

@Serializable data class RouteRequest(val from: LatLngDto, val to: LatLngDto, val alternatives: Boolean = true)

@Serializable
data class StepDto(
    @SerialName("distance_m") val distanceM: Double = 0.0,
    @SerialName("duration_s") val durationS: Double = 0.0,
    val name: String = "",
    val type: String = "",
    val modifier: String = "",
    val exit: Int = 0,
    val at: LatLngDto,
)

@Serializable
data class RouteDto(
    @SerialName("distance_m") val distanceM: Double,
    @SerialName("duration_s") val durationS: Double,
    val geometry: String,
    val steps: List<StepDto> = emptyList(),
)

@Serializable data class RouteResponse(val routes: List<RouteDto> = emptyList())

@Serializable data class PlaceDto(val name: String, val kind: String = "", val lat: Double, val lng: Double)
@Serializable data class PlacesResponse(val items: List<PlaceDto> = emptyList())
