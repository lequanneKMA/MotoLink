package com.motolink.feature.map

import android.content.Context
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import org.maplibre.android.MapLibre
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

/**
 * A route on a MapLibre map.
 *
 * Basemap: NOT decided yet. ADR-014 forbids the public OSM tile servers for releases and requires
 * the Hoàng Sa / Trường Sa check, which is the M1 map spike (M1-T06, decision D-15). Until a style
 * URL is provided by GET /v1/config, a plain background style is used, so the route is drawn without
 * any third-party tiles. Package names assume MapLibre Native 13.x (`org.maplibre.android`) [CHƯA XÁC MINH].
 */
private const val BLANK_STYLE = """{"version":8,"name":"blank","sources":{},"layers":[{"id":"bg","type":"background","paint":{"background-color":"#E6EDE6"}}]}"""
private const val ROUTE_SRC = "route-src"
private const val HEAD_SRC = "head-src"

@Composable
fun RouteMap(points: List<LatLng>, modifier: Modifier = Modifier, followLatest: Boolean = true, styleUrl: String? = null, dark: Boolean = false) {
    val ctx = LocalContext.current
    val mapView = remember { createMapView(ctx) }
    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var styleReady by remember { mutableStateOf(false) }

    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle, mapView) {
        val obs = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_START -> mapView.onStart()
                Lifecycle.Event.ON_RESUME -> mapView.onResume()
                Lifecycle.Event.ON_PAUSE -> mapView.onPause()
                Lifecycle.Event.ON_STOP -> mapView.onStop()
                else -> Unit
            }
        }
        lifecycle.addObserver(obs)
        // The composable may enter the composition after the host already started.
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) mapView.onStart()
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) mapView.onResume()
        onDispose {
            lifecycle.removeObserver(obs)
            mapView.onPause(); mapView.onStop(); mapView.onDestroy()
        }
    }

    LaunchedEffect(mapView, styleUrl, dark) {
        mapView.getMapAsync { m ->
            map = m
            val builder = if (styleUrl.isNullOrBlank()) Style.Builder().fromJson(if (dark) BLANK_STYLE.replace("#E6EDE6", "#1B2420") else BLANK_STYLE) else Style.Builder().fromUri(styleUrl)
            m.setStyle(builder) { style ->
                style.addSource(GeoJsonSource(ROUTE_SRC, FeatureCollection.fromFeatures(emptyList<Feature>())))
                style.addSource(GeoJsonSource(HEAD_SRC, FeatureCollection.fromFeatures(emptyList<Feature>())))
                style.addLayer(LineLayer("route-line", ROUTE_SRC).withProperties(
                    PropertyFactory.lineColor("#1B5E20"), PropertyFactory.lineWidth(5f),
                    PropertyFactory.lineCap("round"), PropertyFactory.lineJoin("round"),
                ))
                style.addLayer(CircleLayer("head-dot", HEAD_SRC).withProperties(
                    PropertyFactory.circleColor("#E53935"), PropertyFactory.circleRadius(7f),
                    PropertyFactory.circleStrokeColor("#FFFFFF"), PropertyFactory.circleStrokeWidth(2f),
                ))
                styleReady = true
            }
        }
    }

    LaunchedEffect(points, styleReady, map) {
        val m = map ?: return@LaunchedEffect
        if (!styleReady) return@LaunchedEffect
        m.getStyle { style ->
            val line = if (points.size >= 2) {
                FeatureCollection.fromFeature(Feature.fromGeometry(LineString.fromLngLats(points.map { Point.fromLngLat(it.longitude, it.latitude) })))
            } else FeatureCollection.fromFeatures(emptyList<Feature>())
            style.getSourceAs<GeoJsonSource>(ROUTE_SRC)?.setGeoJson(line)
            val head = points.lastOrNull()?.let { FeatureCollection.fromFeature(Feature.fromGeometry(Point.fromLngLat(it.longitude, it.latitude))) }
                ?: FeatureCollection.fromFeatures(emptyList<Feature>())
            style.getSourceAs<GeoJsonSource>(HEAD_SRC)?.setGeoJson(head)
        }
        when {
            points.isEmpty() -> Unit
            followLatest -> m.easeCamera(CameraUpdateFactory.newLatLngZoom(points.last(), 16.0))
            points.size == 1 -> m.easeCamera(CameraUpdateFactory.newLatLngZoom(points.first(), 15.0))
            else -> {
                val b = LatLngBounds.Builder().also { bb -> points.forEach { bb.include(it) } }.build()
                m.easeCamera(CameraUpdateFactory.newLatLngBounds(b, 64))
            }
        }
    }

    AndroidView(factory = { mapView }, modifier = modifier.fillMaxSize())
}

private fun createMapView(ctx: Context): MapView {
    MapLibre.getInstance(ctx.applicationContext)
    return MapView(ctx).also { it.onCreate(null) }
}
