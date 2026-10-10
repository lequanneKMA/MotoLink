package com.motolink.feature.home

import android.text.format.DateUtils
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.R
import com.motolink.feature.map.RouteMap
import com.motolink.feature.tracking.toFix
import com.motolink.logic.Format
import com.motolink.logic.RouteMath
import com.motolink.ui.container
import kotlinx.coroutines.launch
import org.maplibre.android.geometry.LatLng

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HistoryScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val repo = container().tracking
    val sessions by repo.recentSessions().collectAsStateWithLifecycle(initialValue = emptyList())
    val ctx = LocalContext.current
    Scaffold(topBar = {
        TopAppBar(
            title = { Text(stringResource(R.string.history_title)) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, null) } },
        )
    }) { pad ->
        if (sessions.isEmpty()) {
            Box(Modifier.padding(pad).fillMaxSize()) { Text(stringResource(R.string.no_history), Modifier.padding(24.dp)) }
        } else {
            LazyColumn(Modifier.padding(pad)) {
                items(sessions, key = { it.sessionId }) { s ->
                    ListItem(
                        headlineContent = { Text(DateUtils.formatDateTime(ctx, s.startedAt, DateUtils.FORMAT_SHOW_DATE or DateUtils.FORMAT_SHOW_TIME or DateUtils.FORMAT_ABBREV_MONTH)) },
                        supportingContent = { Text(if (s.status == "ended") "Đã kết thúc" else "Đang mở") },
                        modifier = Modifier.clickable { onOpen(s.sessionId) },
                    )
                    HorizontalDivider()
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HistoryDetailScreen(sessionId: String, onBack: () -> Unit) {
    val repo = container().tracking
    val points by repo.points(sessionId).collectAsStateWithLifecycle(initialValue = emptyList())
    val scope = rememberCoroutineScope()
    val fixes = remember(points) { points.filter { !it.suspect }.map { it.toFix() } }
    val stats = remember(fixes) { RouteMath.stats(fixes) }
    val latLngs = remember(fixes) { fixes.map { LatLng(it.lat, it.lng) } }
    Scaffold(topBar = {
        TopAppBar(
            title = { Text(stringResource(R.string.history_title)) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, null) } },
            actions = {
                IconButton(onClick = { scope.launch { repo.delete(sessionId); onBack() } }) { Icon(Icons.Filled.Delete, contentDescription = "Xóa") }
            },
        )
    }) { pad ->
        Column(Modifier.padding(pad).fillMaxSize()) {
            Box(Modifier.weight(1f).fillMaxWidth()) { RouteMap(points = latLngs, followLatest = false) }
            Column(Modifier.padding(16.dp)) {
                Text("${stringResource(R.string.stat_distance)}: ${Format.distance(stats.distanceM)}")
                Text("${stringResource(R.string.stat_duration)}: ${Format.duration(stats.durationS)}")
                Text("${stringResource(R.string.stat_points)}: ${stats.pointCount}")
                if (stats.durationS > 0) Text("Tốc độ trung bình: ${Format.speedKmh(stats.avgSpeedMps)}")
            }
        }
    }
}
