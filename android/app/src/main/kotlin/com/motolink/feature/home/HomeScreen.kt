package com.motolink.feature.home

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.feature.map.rememberMapStyleUrl
import com.motolink.feature.map.RouteMap
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.hasLocationConsent
import com.motolink.feature.tracking.rememberPermissionFlow
import com.motolink.logic.Format
import com.motolink.ui.MapControl
import com.motolink.ui.Palette
import com.motolink.ui.PrimaryButton
import com.motolink.ui.SecondaryButton
import com.motolink.ui.SpeedDial
import com.motolink.ui.container
import com.motolink.ui.containerViewModel
import kotlinx.coroutines.launch

/**
 * Solo recording: the map fills the screen; a menu button, own speed and one bottom sheet are all that floats on it
 * (docs/design-ui, "Đi một mình"). Group riding lives in the trips list.
 */
@Composable
fun HomeScreen(onHistory: () -> Unit, onSettings: () -> Unit, onTrips: () -> Unit, onNotifications: () -> Unit, onNavigate: () -> Unit) {
    val ctx = LocalContext.current
    val c = container()
    val vm = containerViewModel { HomeViewModel(it.tracking) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var menuOpen by remember { mutableStateOf(false) }
    val flow = rememberPermissionFlow(onReady = { vm.prepareSession { TrackingService.start(ctx) } })
    val recording = ui.state != RecState.IDLE

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        RouteMap(points = ui.route, followLatest = ui.state == RecState.ACTIVE, dark = isSystemInDarkTheme(), styleUrl = rememberMapStyleUrl())

        MapControl(Icons.Filled.Menu, "Mở menu", { menuOpen = true }, Modifier.align(Alignment.TopStart).statusBarsPadding().padding(12.dp))
        if (recording) SpeedDial(ui.speedKmh, Modifier.align(Alignment.TopEnd).statusBarsPadding().padding(12.dp))

        Surface(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth(), shape = RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp),
            color = MaterialTheme.colorScheme.surface, shadowElevation = 12.dp,
        ) {
            Column(Modifier.navigationBarsPadding().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Box(Modifier.align(Alignment.CenterHorizontally).width(44.dp).heightIn(min = 5.dp).background(MaterialTheme.colorScheme.outline, RoundedCornerShape(3.dp)))
                ModeSwitch(group = false, onSolo = {}, onGroup = onTrips)
                if (!recording) DestinationBar(onNavigate)

                if (recording) {
                    Text(
                        when (ui.state) {
                            RecState.ACTIVE -> if (ui.gpsAvailable) "Đang ghi hành trình" else "Mất tín hiệu GPS"
                            RecState.PAUSED -> "Đã tạm dừng"
                            else -> "Việc ghi bị gián đoạn"
                        },
                        style = MaterialTheme.typography.titleMedium,
                        color = if (ui.state == RecState.INTERRUPTED || !ui.gpsAvailable) Palette.Red else MaterialTheme.colorScheme.onSurface,
                    )
                    if (ui.state == RecState.INTERRUPTED) {
                        Text("Hệ điều hành có thể đã tắt ứng dụng. Xem hướng dẫn trong Cài đặt để hạn chế điều này.", style = MaterialTheme.typography.bodyMedium)
                    }
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Stat("Quãng đường", Format.distance(ui.stats.distanceM))
                        Stat("Thời gian", Format.duration(ui.stats.durationS))
                        Stat("Số điểm", ui.stats.pointCount.toString())
                    }
                }
                Controls(
                    state = ui.state,
                    onStart = { scope.launch { flow.begin(consented = c.prefs.hasLocationConsent()) } },
                    onPause = { TrackingService.pause(ctx) }, onResume = { TrackingService.resume(ctx) },
                    onEnd = { TrackingService.end(ctx) }, onRestart = { TrackingService.start(ctx) },
                )
            }
        }

        if (menuOpen) {
            Box(Modifier.fillMaxSize().background(Color(0x99000000)).clickable { menuOpen = false }) {
                Column(
                    Modifier.fillMaxHeight().width(300.dp).background(MaterialTheme.colorScheme.surface).statusBarsPadding().navigationBarsPadding().clickable(enabled = false) {},
                ) {
                    Text("MotoLink", style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(20.dp))
                    MenuRow(Icons.Filled.Groups, "Chuyến đi của tôi") { menuOpen = false; onTrips() }
                    MenuRow(Icons.Filled.History, "Lịch sử hành trình") { menuOpen = false; onHistory() }
                    MenuRow(Icons.Filled.Notifications, "Thông báo") { menuOpen = false; onNotifications() }
                    MenuRow(Icons.Filled.Settings, "Cài đặt") { menuOpen = false; onSettings() }
                }
            }
        }
    }
}

/** "Bạn muốn đi đâu?" — opens search, then route choice and turn-by-turn. */
@Composable
private fun DestinationBar(onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 62.dp).background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(16.dp)).clickable(onClick = onClick).padding(horizontal = 18.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("Bạn muốn đi đâu?", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
        Icon(Icons.Filled.Search, null, Modifier.size(30.dp))
    }
}

@Composable
private fun MenuRow(icon: ImageVector, label: String, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 68.dp).clickable(onClick = onClick).padding(horizontal = 20.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Box(Modifier.size(44.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(14.dp)), contentAlignment = Alignment.Center) {
            Icon(icon, null, Modifier.size(26.dp), tint = MaterialTheme.colorScheme.primary)
        }
        Text(label, style = MaterialTheme.typography.bodyLarge)
    }
}

/** "Đi một mình | Đi nhóm": two large segments, the active one filled. */
@Composable
fun ModeSwitch(group: Boolean, onSolo: () -> Unit, onGroup: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(16.dp)).padding(4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        @Composable
        fun Seg(label: String, active: Boolean, onClick: () -> Unit, modifier: Modifier) {
            Box(
                modifier.heightIn(min = 52.dp).background(if (active) MaterialTheme.colorScheme.primary else Color.Transparent, RoundedCornerShape(12.dp)).clickable(onClick = onClick),
                contentAlignment = Alignment.Center,
            ) {
                Text(label, color = if (active) Color.White else MaterialTheme.colorScheme.onSurface, fontSize = 17.sp, fontWeight = FontWeight.ExtraBold)
            }
        }
        Seg("Đi một mình", !group, onSolo, Modifier.weight(1f))
        Seg("Đi nhóm", group, onGroup, Modifier.weight(1f))
    }
}

@Composable
private fun Stat(label: String, value: String) = Column {
    Text(value, style = MaterialTheme.typography.titleLarge)
    Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun Controls(state: RecState, onStart: () -> Unit, onPause: () -> Unit, onResume: () -> Unit, onEnd: () -> Unit, onRestart: () -> Unit) {
    when (state) {
        RecState.IDLE -> PrimaryButton("Bắt đầu ghi hành trình", onStart)
        RecState.ACTIVE -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            SecondaryButton("Tạm dừng", onPause, Modifier.weight(1f))
            PrimaryButton("Kết thúc", onEnd, Modifier.weight(1f))
        }
        RecState.PAUSED -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            PrimaryButton("Tiếp tục", onResume, Modifier.weight(1f))
            SecondaryButton("Kết thúc", onEnd, Modifier.weight(1f))
        }
        RecState.INTERRUPTED -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            PrimaryButton("Ghi tiếp", onRestart, Modifier.weight(1f))
            SecondaryButton("Kết thúc", onEnd, Modifier.weight(1f))
        }
    }
}
