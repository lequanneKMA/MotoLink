package com.motolink.feature.map

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.motolink.ui.MemberAvatar
import com.motolink.ui.Palette
import org.maplibre.android.MapLibre
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * A member (or SOS) on the map. [selfArrow] draws the rider's own heading arrow instead of an initial.
 * Markers are Compose overlays positioned from the map projection, so they can carry letters and labels without
 * a glyph server (the basemap itself is still undecided, ADR-014 / D-15).
 */
data class MapMarker(
    val id: String,
    val at: LatLng,
    val initial: String,
    val color: Color,
    val fade: Float = 1f,
    val label: String? = null,
    val isSos: Boolean = false,
    val selfArrow: Boolean = false,
)

private fun blankStyle(dark: Boolean): String {
    val bg = if (dark) "#1B2420" else "#E9EEE6"
    return """{"version":8,"name":"blank","sources":{},"layers":[{"id":"bg","type":"background","paint":{"background-color":"$bg"}}]}"""
}

/**
 * Members of a trip on a map. Package names assume MapLibre Native 13.x [CHƯA XÁC MINH].
 * [fitSignal] > 0 and changing re-frames everybody; the first non-empty marker set is framed automatically.
 */
@Composable
fun LiveMap(
    markers: List<MapMarker>,
    fitSignal: Int,
    modifier: Modifier = Modifier,
    dark: Boolean = false,
    styleUrl: String? = null,
    onMarkerClick: ((String) -> Unit)? = null,
) {
    val ctx = LocalContext.current
    val density = LocalDensity.current
    val mapView = remember { MapLibre.getInstance(ctx.applicationContext); MapView(ctx).also { it.onCreate(null) } }
    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var styleReady by remember { mutableStateOf(false) }
    var fittedOnce by remember { mutableStateOf(false) }
    var cameraTick by remember { mutableIntStateOf(0) }

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
            m.uiSettings.isAttributionEnabled = true
            m.addOnCameraMoveListener { cameraTick++ }
            m.addOnCameraIdleListener { cameraTick++ }
            val builder = if (styleUrl.isNullOrBlank()) Style.Builder().fromJson(blankStyle(dark)) else Style.Builder().fromUri(styleUrl)
            m.setStyle(builder) { styleReady = true; cameraTick++ }
        }
    }

    LaunchedEffect(markers, styleReady, map) {
        val m = map ?: return@LaunchedEffect
        if (!styleReady) return@LaunchedEffect
        cameraTick++
        if (!fittedOnce && markers.isNotEmpty()) {
            fittedOnce = true
            fit(m, markers)
        }
    }
    LaunchedEffect(fitSignal) {
        val m = map ?: return@LaunchedEffect
        if (fitSignal > 0 && markers.isNotEmpty()) fit(m, markers)
    }

    Box(modifier.fillMaxSize()) {
        AndroidView(factory = { mapView }, modifier = Modifier.fillMaxSize())
        val m = map
        if (m != null && styleReady) {
            val tick = cameraTick // reading the state makes the overlay follow every camera change
            if (tick < 0) return@Box
            markers.forEach { mk ->
                val p = runCatching { m.projection.toScreenLocation(mk.at) }.getOrNull() ?: return@forEach
                val half = with(density) { (if (mk.isSos) 36.dp else 20.dp).roundToPx() }
                Box(
                    Modifier.offset { IntOffset(p.x.toInt() - half, p.y.toInt() - half) }
                        .let { if (onMarkerClick != null) it.clickable { onMarkerClick(mk.id) } else it },
                    contentAlignment = Alignment.Center,
                ) { MarkerView(mk) }
            }
        }
    }
}

@Composable
private fun MarkerView(mk: MapMarker) {
    Box(contentAlignment = Alignment.Center) {
        if (mk.isSos) Box(Modifier.size(72.dp).background(Palette.Red.copy(alpha = 0.22f), CircleShape))
        if (mk.selfArrow) {
            Box(Modifier.size(40.dp).background(mk.color, CircleShape).padding(2.dp), contentAlignment = Alignment.Center) {
                Text("▲", color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Black)
            }
        } else {
            MemberAvatar(mk.initial, if (mk.isSos) Palette.Red else mk.color, size = 40.dp, alpha = mk.fade)
        }
        mk.label?.let {
            Text(
                it, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.ExtraBold, maxLines = 1,
                modifier = Modifier.offset(y = 38.dp).alpha(mk.fade).background(Color(0xCC111111), RoundedCornerShape(10.dp)).padding(horizontal = 8.dp, vertical = 3.dp),
            )
        }
    }
}

private fun fit(m: MapLibreMap, markers: List<MapMarker>) {
    if (markers.size == 1) {
        m.easeCamera(CameraUpdateFactory.newLatLngZoom(markers.first().at, 15.0))
    } else {
        val b = LatLngBounds.Builder().also { bb -> markers.forEach { bb.include(it.at) } }.build()
        m.easeCamera(CameraUpdateFactory.newLatLngBounds(b, 140))
    }
}
