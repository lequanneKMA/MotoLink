package com.motolink.feature.home

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.R
import com.motolink.feature.map.RouteMap
import com.motolink.feature.tracking.TrackingService
import com.motolink.logic.Format
import com.motolink.feature.tracking.rememberPermissionFlow
import com.motolink.feature.tracking.hasLocationConsent
import com.motolink.ui.container
import com.motolink.ui.containerViewModel
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(onHistory: () -> Unit, onSettings: () -> Unit, onTrips: () -> Unit, onNotifications: () -> Unit) {
    val ctx = LocalContext.current
    val c = container()
    val vm = containerViewModel { HomeViewModel(it.tracking) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    // Called when permissions are in place; creates the session and starts the foreground service.
    val flow = rememberPermissionFlow(onReady = { vm.prepareSession { TrackingService.start(ctx) } })

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.home_title)) },
                actions = {
                    IconButton(onClick = onTrips) { Icon(Icons.Filled.Groups, contentDescription = "Chuyến đi") }
                    IconButton(onClick = onNotifications) { Icon(Icons.Filled.Notifications, contentDescription = "Thông báo") }
                    IconButton(onClick = onHistory) { Icon(Icons.Filled.History, contentDescription = stringResource(R.string.history_title)) }
                    IconButton(onClick = onSettings) { Icon(Icons.Filled.Settings, contentDescription = stringResource(R.string.settings_title)) }
                },
            )
        },
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize()) {
            Box(Modifier.weight(1f).fillMaxWidth()) {
                RouteMap(points = ui.route, followLatest = ui.state == RecState.ACTIVE)
            }
            Card(Modifier.fillMaxWidth().padding(12.dp)) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        when (ui.state) {
                            RecState.IDLE -> stringResource(R.string.status_idle)
                            RecState.ACTIVE -> stringResource(R.string.status_active)
                            RecState.PAUSED -> stringResource(R.string.status_paused)
                            RecState.INTERRUPTED -> "Việc ghi bị gián đoạn" + ui.session?.let { " (từ " + DateUtils.formatDateTime(ctx, it.startedAt, DateUtils.FORMAT_SHOW_TIME) + ")" }.orEmpty()
                        },
                        style = MaterialTheme.typography.titleMedium,
                    )
                    if (ui.state == RecState.INTERRUPTED) {
                        Text("Hệ điều hành có thể đã tắt ứng dụng. Xem hướng dẫn trong Cài đặt để hạn chế điều này.", style = MaterialTheme.typography.bodySmall)
                    }
                    if (!ui.gpsAvailable && ui.state == RecState.ACTIVE) {
                        Text("Mất tín hiệu GPS hoặc GPS đang tắt", color = MaterialTheme.colorScheme.error)
                    }
                    if (ui.state != RecState.IDLE) StatsRow(ui)
                    Controls(
                        state = ui.state,
                        onStart = { scope.launch { flow.begin(consented = c.prefs.hasLocationConsent()) } },
                        onPause = { TrackingService.pause(ctx) },
                        onResume = { TrackingService.resume(ctx) },
                        onEnd = { TrackingService.end(ctx) },
                        onRestart = { TrackingService.start(ctx) },
                    )
                }
            }
        }
    }
}

@Composable
private fun StatsRow(ui: HomeUi) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Stat(stringResource(R.string.stat_distance), Format.distance(ui.stats.distanceM))
        Stat(stringResource(R.string.stat_duration), Format.duration(ui.stats.durationS))
        Stat(stringResource(R.string.stat_points), ui.stats.pointCount.toString())
    }
}

@Composable
private fun Stat(label: String, value: String) = Column {
    Text(value, style = MaterialTheme.typography.titleLarge)
    Text(label, style = MaterialTheme.typography.labelSmall)
}

@Composable
private fun Controls(state: RecState, onStart: () -> Unit, onPause: () -> Unit, onResume: () -> Unit, onEnd: () -> Unit, onRestart: () -> Unit) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        when (state) {
            RecState.IDLE -> Button(onClick = onStart, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text(stringResource(R.string.action_start)) }
            RecState.ACTIVE -> {
                OutlinedButton(onClick = onPause, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_pause)) }
                Button(onClick = onEnd, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_end)) }
            }
            RecState.PAUSED -> {
                Button(onClick = onResume, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_resume)) }
                OutlinedButton(onClick = onEnd, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_end)) }
            }
            RecState.INTERRUPTED -> {
                Button(onClick = onRestart, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_resume)) }
                OutlinedButton(onClick = onEnd, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.action_end)) }
            }
        }
    }
}
