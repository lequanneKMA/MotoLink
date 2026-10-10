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
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.Point

/** A member marker. [fade] dims stale/lost positions; names are shown in the list next to the map (no glyphs without a basemap). */
data class MapMarker(val id: String, val at: LatLng, val colorHex: String, val fade: Float = 1f, val isSos: Boolean = false)

private const val BLANK_STYLE = """{"version":8,"name":"blank","sources":{},"layers":[{"id":"bg","type":"background","paint":{"background-color":"#E6EDE6"}}]}"""
private const val MARKERS = "markers-src"

/**
 * Members of a trip on a map. Same basemap policy as [RouteMap] (ADR-014, D-15): blank until a style URL is configured.
 * Package names assume MapLibre Native 13.x [CHƯA XÁC MINH].
 */
@Composable
fun LiveMap(markers: List<MapMarker>, fitSignal: Int, modifier: Modifier = Modifier, styleUrl: String? = null) {
    val ctx = LocalContext.current
    val mapView = remember { MapLibre.getInstance(ctx.applicationContext); MapView(ctx).also { it.onCreate(null) } }
    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var styleReady by remember { mutableStateOf(false) }
    var fittedOnce by remember { mutableStateOf(false) }

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
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) mapView.onStart()
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) mapView.onResume()
        onDispose {
            lifecycle.removeObserver(obs)
            mapView.onPause(); mapView.onStop(); mapView.onDestroy()
        }
    }

    LaunchedEffect(mapView, styleUrl) {
        mapView.getMapAsync { m ->
            map = m
            val builder = if (styleUrl.isNullOrBlank()) Style.Builder().fromJson(BLANK_STYLE) else Style.Builder().fromUri(styleUrl)
            m.setStyle(builder) { style ->
                style.addSource(GeoJsonSource(MARKERS, FeatureCollection.fromFeatures(emptyList<Feature>())))
                style.addLayer(
                    CircleLayer("marker-sos-ring", MARKERS).withFilter(Expression.eq(Expression.get("sos"), true)).withProperties(
                        PropertyFactory.circleColor("#D50000"), PropertyFactory.circleOpacity(0.25f), PropertyFactory.circleRadius(22f),
                    ),
                )
                style.addLayer(
                    CircleLayer("marker-dot", MARKERS).withProperties(
                        PropertyFactory.circleColor(Expression.toColor(Expression.get("color"))),
                        PropertyFactory.circleOpacity(Expression.get("fade")),
                        PropertyFactory.circleRadius(9f),
                        PropertyFactory.circleStrokeColor("#FFFFFF"), PropertyFactory.circleStrokeWidth(2.5f),
                    ),
                )
                styleReady = true
            }
        }
    }

    LaunchedEffect(markers, styleReady, map) {
        val m = map ?: return@LaunchedEffect
        if (!styleReady) return@LaunchedEffect
        m.getStyle { style ->
            val fc = FeatureCollection.fromFeatures(
                markers.map {
                    Feature.fromGeometry(Point.fromLngLat(it.at.longitude, it.at.latitude)).also { f ->
                        f.addStringProperty("color", it.colorHex)
                        f.addNumberProperty("fade", it.fade)
                        f.addBooleanProperty("sos", it.isSos)
                    }
                },
            )
            style.getSourceAs<GeoJsonSource>(MARKERS)?.setGeoJson(fc)
        }
        if (!fittedOnce && markers.isNotEmpty()) {
            fittedOnce = true
            fit(m, markers)
        }
    }
    // Explicit "fit everyone" request from the UI.
    LaunchedEffect(fitSignal) {
        val m = map ?: return@LaunchedEffect
        if (fitSignal > 0 && markers.isNotEmpty()) fit(m, markers)
    }

    AndroidView(factory = { mapView }, modifier = modifier.fillMaxSize())
}

private fun fit(m: MapLibreMap, markers: List<MapMarker>) {
    if (markers.size == 1) {
        m.easeCamera(CameraUpdateFactory.newLatLngZoom(markers.first().at, 15.0))
    } else {
        val b = LatLngBounds.Builder().also { bb -> markers.forEach { bb.include(it.at) } }.build()
        m.easeCamera(CameraUpdateFactory.newLatLngBounds(b, 96))
    }
}
