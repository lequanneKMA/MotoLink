package com.motolink.feature.nav

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.Merge
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.filled.RoundaboutRight
import androidx.compose.material.icons.filled.TurnLeft
import androidx.compose.material.icons.filled.TurnRight
import androidx.compose.material.icons.filled.TurnSharpLeft
import androidx.compose.material.icons.filled.TurnSharpRight
import androidx.compose.material.icons.filled.TurnSlightLeft
import androidx.compose.material.icons.filled.TurnSlightRight
import androidx.compose.material.icons.filled.UTurnLeft
import androidx.compose.material.icons.filled.VolumeOff
import androidx.compose.material.icons.filled.VolumeUp
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.motolink.core.location.LocationPermissions
import com.motolink.feature.map.NavMap
import com.motolink.feature.map.rememberMapStyleUrl
import com.motolink.logic.Maneuver
import com.motolink.logic.NavFormat
import com.motolink.logic.NavText
import com.motolink.logic.RideText
import com.motolink.ui.MapControl
import com.motolink.ui.PageHeader
import com.motolink.ui.PrimaryButton
import com.motolink.ui.SecondaryButton
import com.motolink.ui.SpeedDial
import com.motolink.ui.container
import androidx.compose.foundation.layout.width
import org.maplibre.android.geometry.LatLng

fun Maneuver.icon(): ImageVector = when (this) {
    Maneuver.DEPART, Maneuver.STRAIGHT -> Icons.Filled.ArrowUpward
    Maneuver.LEFT -> Icons.Filled.TurnLeft
    Maneuver.RIGHT -> Icons.Filled.TurnRight
    Maneuver.SLIGHT_LEFT -> Icons.Filled.TurnSlightLeft
    Maneuver.SLIGHT_RIGHT -> Icons.Filled.TurnSlightRight
    Maneuver.SHARP_LEFT -> Icons.Filled.TurnSharpLeft
    Maneuver.SHARP_RIGHT -> Icons.Filled.TurnSharpRight
    Maneuver.UTURN -> Icons.Filled.UTurnLeft
    Maneuver.ROUNDABOUT -> Icons.Filled.RoundaboutRight
    Maneuver.MERGE -> Icons.Filled.Merge
    Maneuver.ARRIVE -> Icons.Filled.Flag
}

/** Where to? → choose a route → turn-by-turn. Foreground navigation: the screen stays on and the voice speaks. */
@Composable
fun NavScreen(onClose: () -> Unit) {
    val ctx = LocalContext.current
    val vm: NavViewModel = viewModel()
    val ui by vm.ui.collectAsStateWithLifecycle()
    val cfg by container().mapConfig.state.collectAsStateWithLifecycle()
    val snack = remember { SnackbarHostState() }
    val perm = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { vm.locate() }

    LaunchedEffect(Unit) {
        if (!LocationPermissions.read(ctx).canRecord) perm.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
        else vm.locate()
    }
    LaunchedEffect(ui.message) { ui.message?.let { snack.showSnackbar(it); vm.messageShown() } }

    // the screen must not turn off while a route is being followed
    val view = LocalView.current
    DisposableEffect(ui.stage) {
        view.keepScreenOn = ui.stage == NavStage.NAVIGATING
        onDispose { view.keepScreenOn = false }
    }

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        when (ui.stage) {
            NavStage.SEARCH -> SearchPage(ui, cfg.search && cfg.routing, vm, onClose)
            NavStage.PICK -> PickPage(ui, vm)
            NavStage.NAVIGATING -> NavigatingPage(ui, vm)
            NavStage.ARRIVED -> ArrivedPage(ui.dest?.name.orEmpty()) { vm.end(); onClose() }
        }
        SnackbarHost(snack, Modifier.align(Alignment.BottomCenter).padding(bottom = 24.dp))
    }
}

@Composable
private fun SearchPage(ui: NavUi, enabled: Boolean, vm: NavViewModel, onClose: () -> Unit) {
    val focus = remember { FocusRequester() }
    LaunchedEffect(enabled) { if (enabled) runCatching { focus.requestFocus() } }
    Column(Modifier.fillMaxSize().navigationBarsPadding()) {
        PageHeader("Bạn muốn đi đâu?", onClose)
        if (!enabled) {
            Text(
                "Chỉ đường chưa được bật trên máy chủ. Cần cấu hình OSRM_URL và GEOCODE_DATABASE_URL (xem deploy/maps/README.md).",
                style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(20.dp),
            )
            return@Column
        }
        OutlinedTextField(
            value = ui.query, onValueChange = vm::setQuery, singleLine = true,
            placeholder = { Text("Tìm địa điểm, tên đường", style = MaterialTheme.typography.bodyLarge) },
            textStyle = MaterialTheme.typography.bodyLarge, shape = RoundedCornerShape(14.dp),
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).heightIn(min = 62.dp).focusRequester(focus),
        )
        if (ui.searching) CircularProgressIndicator(Modifier.padding(20.dp).size(28.dp))
        LazyColumn(Modifier.weight(1f).padding(top = 8.dp)) {
            items(ui.results, key = { "${it.name}${it.lat}${it.lng}" }) { p ->
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 68.dp).clickable { vm.pick(p) }.padding(horizontal = 20.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    Icon(Icons.Filled.Place, null, Modifier.size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Column {
                        Text(p.name, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        if (p.kind.isNotBlank()) Text(kindLabel(p.kind), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                HorizontalDivider(color = MaterialTheme.colorScheme.outline)
            }
        }
    }
}

private fun kindLabel(kind: String) = when (kind) {
    "road" -> "Đường"; "city" -> "Thành phố"; "town" -> "Thị trấn"; "village", "hamlet" -> "Làng, thôn"; "suburb", "neighbourhood" -> "Khu vực"
    "restaurant", "cafe", "fast_food" -> "Ăn uống"; "fuel" -> "Trạm xăng"; "hotel", "guest_house", "hostel" -> "Lưu trú"
    "tourism", "attraction", "viewpoint" -> "Du lịch"; else -> kind.replace('_', ' ')
}

@Composable
private fun PickPage(ui: NavUi, vm: NavViewModel) {
    val dark = isSystemInDarkTheme()
    val routes = ui.options.map { o -> o.route.points.map { LatLng(it.lat, it.lng) } }
    Box(Modifier.fillMaxSize()) {
        NavMap(
            routes = routes, selected = ui.selected, me = ui.me?.let { LatLng(it.lat, it.lng) },
            dest = ui.dest?.let { LatLng(it.lat, it.lng) }, follow = false, bearing = null, dark = dark, styleUrl = rememberMapStyleUrl(),
        )
        MapControl(Icons.AutoMirrored.Filled.ArrowBack, "Quay lại", vm::backToSearch, Modifier.align(Alignment.TopStart).statusBarsPadding().padding(12.dp))
        Surface(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth(), shape = RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp),
            color = MaterialTheme.colorScheme.surface, shadowElevation = 12.dp,
        ) {
            Column(Modifier.navigationBarsPadding().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(ui.dest?.name.orEmpty(), style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (ui.loading) {
                    Row(Modifier.padding(vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(28.dp)); Text("Đang tìm đường…", style = MaterialTheme.typography.bodyLarge)
                    }
                }
                ui.options.forEachIndexed { i, o ->
                    val sel = i == ui.selected
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 68.dp).background(if (sel) MaterialTheme.colorScheme.primaryContainer else Color.Transparent, RoundedCornerShape(14.dp))
                            .clickable { vm.select(i) }.padding(horizontal = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(o.label, style = MaterialTheme.typography.bodyLarge)
                            Text(RideText.distance(o.distanceM), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(NavFormat.duration(o.durationS), style = MaterialTheme.typography.titleMedium)
                    }
                }
                if (ui.options.isNotEmpty()) PrimaryButton("Bắt đầu", vm::start, icon = Icons.Filled.PlayArrow)
            }
        }
    }
}

@Composable
private fun NavigatingPage(ui: NavUi, vm: NavViewModel) {
    val dark = isSystemInDarkTheme()
    val opt = ui.options.getOrNull(ui.selected)
    val st = ui.nav
    val step = st?.nextStep?.let { opt?.route?.steps?.getOrNull(it) }
    Box(Modifier.fillMaxSize()) {
        NavMap(
            routes = ui.options.map { o -> o.route.points.map { LatLng(it.lat, it.lng) } }.take(1), selected = 0, me = ui.me?.let { LatLng(it.lat, it.lng) },
            dest = ui.dest?.let { LatLng(it.lat, it.lng) }, follow = true, bearing = ui.bearing, dark = dark, styleUrl = rememberMapStyleUrl(),
        )
        // turn banner: the one thing to read while riding
        Surface(Modifier.align(Alignment.TopCenter).fillMaxWidth(), color = Color(0xFF0B3D1E), shadowElevation = 8.dp) {
            Row(
                Modifier.statusBarsPadding().padding(horizontal = 16.dp, vertical = 12.dp).heightIn(min = 84.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Icon(if (step != null) NavText.maneuver(step).icon() else Icons.Filled.ArrowUpward, null, Modifier.size(56.dp), tint = Color.White)
                Column {
                    Text(RideText.distance(st?.distToNextM ?: 0.0), color = Color.White, fontSize = 34.sp, fontWeight = FontWeight.Black, lineHeight = 36.sp)
                    Text(
                        if (ui.rerouting) "Đang tìm đường mới…" else step?.let { NavText.instruction(it) } ?: "Đi theo đường",
                        color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
        SpeedDial(ui.speedKmh, Modifier.align(Alignment.BottomStart).padding(start = 16.dp, bottom = 110.dp).navigationBarsPadding())
        MapControl(
            if (ui.voice) Icons.Filled.VolumeUp else Icons.Filled.VolumeOff, if (ui.voice) "Tắt giọng nói" else "Bật giọng nói", vm::toggleVoice,
            Modifier.align(Alignment.BottomEnd).padding(end = 16.dp, bottom = 110.dp).navigationBarsPadding(),
        )
        Surface(Modifier.align(Alignment.BottomCenter).fillMaxWidth(), shape = RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp), color = MaterialTheme.colorScheme.surface, shadowElevation = 12.dp) {
            Row(Modifier.navigationBarsPadding().padding(horizontal = 20.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(NavFormat.duration(st?.remainingS ?: opt?.durationS ?: 0.0), style = MaterialTheme.typography.titleLarge)
                    Text(
                        RideText.distance(st?.remainingM ?: opt?.distanceM ?: 0.0) + " · đến lúc " +
                            NavFormat.arrival(st?.remainingS ?: 0.0, System.currentTimeMillis(), java.util.TimeZone.getDefault().getOffset(System.currentTimeMillis()).toLong()),
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                SecondaryButton("Kết thúc", vm::end, Modifier.width(150.dp))
            }
        }
    }
}

@Composable
private fun ArrivedPage(name: String, onDone: () -> Unit) {
    Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Box(Modifier.size(112.dp).background(MaterialTheme.colorScheme.primary, CircleShape), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.Flag, null, Modifier.size(60.dp), tint = Color.White)
            }
            Text("Bạn đã đến nơi", style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(top = 24.dp))
            Text(name, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 8.dp))
        }
        PrimaryButton("Xong", onDone)
    }
}
