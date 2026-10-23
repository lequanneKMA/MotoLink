package com.motolink.feature.map

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
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

private const val BLANK = """{"version":8,"name":"blank","sources":{},"layers":[{"id":"bg","type":"background","paint":{"background-color":"#E9EEE6"}}]}"""
private const val BLANK_DARK = """{"version":8,"name":"blank","sources":{},"layers":[{"id":"bg","type":"background","paint":{"background-color":"#1B2420"}}]}"""

/**
 * Route options and the rider on a map. [follow] keeps the camera on the rider, tilted and turned to their heading
 * (navigation); otherwise the camera frames the selected route (choosing a route). Package names assume MapLibre
 * Native 13.x [CHƯA XÁC MINH].
 */
@Composable
fun NavMap(
    routes: List<List<LatLng>>, selected: Int, me: LatLng?, dest: LatLng?, follow: Boolean, bearing: Float?,
    modifier: Modifier = Modifier, dark: Boolean = false, styleUrl: String? = null,
) {
    val ctx = LocalContext.current
    val mapView = remember { MapLibre.getInstance(ctx.applicationContext); MapView(ctx).also { it.onCreate(null) } }
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
            m.uiSettings.isLogoEnabled = false
            styleReady = false
            val builder = if (styleUrl.isNullOrBlank()) Style.Builder().fromJson(if (dark) BLANK_DARK else BLANK) else Style.Builder().fromUri(styleUrl)
            m.setStyle(builder) { style ->
                val empty = FeatureCollection.fromFeatures(emptyList<Feature>())
                style.addSource(GeoJsonSource("alt-src", empty))
                style.addSource(GeoJsonSource("sel-src", empty))
                style.addSource(GeoJsonSource("pts-src", empty))
                style.addLayer(LineLayer("alt-line", "alt-src").withProperties(
                    PropertyFactory.lineColor(if (dark) "#6C7F93" else "#8FA3B8"), PropertyFactory.lineWidth(6f),
                    PropertyFactory.lineCap("round"), PropertyFactory.lineJoin("round"),
                ))
                style.addLayer(LineLayer("sel-casing", "sel-src").withProperties(
                    PropertyFactory.lineColor(if (dark) "#0B1B33" else "#FFFFFF"), PropertyFactory.lineWidth(12f),
                    PropertyFactory.lineCap("round"), PropertyFactory.lineJoin("round"),
                ))
                style.addLayer(LineLayer("sel-line", "sel-src").withProperties(
                    PropertyFactory.lineColor("#1A73E8"), PropertyFactory.lineWidth(8f),
                    PropertyFactory.lineCap("round"), PropertyFactory.lineJoin("round"),
                ))
                style.addLayer(CircleLayer("dest-dot", "pts-src").withFilter(Expression.eq(Expression.get("kind"), "dest")).withProperties(
                    PropertyFactory.circleColor("#D50000"), PropertyFactory.circleRadius(11f),
                    PropertyFactory.circleStrokeColor("#FFFFFF"), PropertyFactory.circleStrokeWidth(3f),
                ))
                style.addLayer(CircleLayer("me-dot", "pts-src").withFilter(Expression.eq(Expression.get("kind"), "me")).withProperties(
                    PropertyFactory.circleColor("#1565C0"), PropertyFactory.circleRadius(10f),
                    PropertyFactory.circleStrokeColor("#FFFFFF"), PropertyFactory.circleStrokeWidth(3.5f),
                ))
                styleReady = true
            }
        }
    }

    LaunchedEffect(routes, selected, me, dest, styleReady, map) {
        val m = map ?: return@LaunchedEffect
        if (!styleReady) return@LaunchedEffect
        m.getStyle { style ->
            fun line(pts: List<LatLng>) = Feature.fromGeometry(LineString.fromLngLats(pts.map { Point.fromLngLat(it.longitude, it.latitude) }))
            val alt = routes.filterIndexed { i, r -> i != selected && r.size >= 2 }.map(::line)
            style.getSourceAs<GeoJsonSource>("alt-src")?.setGeoJson(FeatureCollection.fromFeatures(alt))
            val sel = routes.getOrNull(selected)?.takeIf { it.size >= 2 }?.let { FeatureCollection.fromFeature(line(it)) }
                ?: FeatureCollection.fromFeatures(emptyList<Feature>())
            style.getSourceAs<GeoJsonSource>("sel-src")?.setGeoJson(sel)
            val pts = buildList {
                me?.let { add(Feature.fromGeometry(Point.fromLngLat(it.longitude, it.latitude)).also { f -> f.addStringProperty("kind", "me") }) }
                dest?.let { add(Feature.fromGeometry(Point.fromLngLat(it.longitude, it.latitude)).also { f -> f.addStringProperty("kind", "dest") }) }
            }
            style.getSourceAs<GeoJsonSource>("pts-src")?.setGeoJson(FeatureCollection.fromFeatures(pts))
        }
    }

    // camera: follow the rider while navigating, otherwise frame the chosen route
    LaunchedEffect(follow, me, bearing, routes, selected, styleReady, map) {
        val m = map ?: return@LaunchedEffect
        if (!styleReady) return@LaunchedEffect
        if (follow) {
            val target = me ?: return@LaunchedEffect
            val cam = CameraPosition.Builder().target(target).zoom(17.0).tilt(45.0).bearing((bearing ?: 0f).toDouble()).build()
            m.easeCamera(CameraUpdateFactory.newCameraPosition(cam), 900)
        } else {
            val pts = routes.getOrNull(selected).orEmpty()
            if (pts.size >= 2) {
                val b = LatLngBounds.Builder().also { bb -> pts.forEach { bb.include(it) }; me?.let { bb.include(it) } }.build()
                m.easeCamera(CameraUpdateFactory.newLatLngBounds(b, 120), 600)
            } else if (me != null) {
                m.easeCamera(CameraUpdateFactory.newLatLngZoom(me, 14.0), 600)
            }
        }
    }

    AndroidView(factory = { mapView }, modifier = modifier.fillMaxSize())
}
