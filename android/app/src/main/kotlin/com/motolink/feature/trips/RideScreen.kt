package com.motolink.feature.trips

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.feature.map.LiveMap
import com.motolink.feature.map.MapMarker
import com.motolink.feature.realtime.ConnState
import com.motolink.feature.sos.ReceiveSosPage
import com.motolink.feature.sos.SendSosPage
import com.motolink.feature.sos.SentState
import com.motolink.feature.sos.SosSentPage
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.hasLocationConsent
import com.motolink.feature.tracking.rememberPermissionFlow
import com.motolink.logic.GroupWatch
import com.motolink.logic.MemberLive
import com.motolink.logic.MemberStatus
import com.motolink.ui.DangerButton
import com.motolink.ui.MemberAvatar
import com.motolink.ui.Palette
import com.motolink.ui.SecondaryButton
import com.motolink.ui.SosFab
import com.motolink.ui.SpeedDial
import com.motolink.ui.SwitchRow
import com.motolink.ui.container
import com.motolink.ui.containerViewModel
import com.motolink.ui.memberColor
import com.motolink.ui.parseHex
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.maplibre.android.geometry.LatLng

private const val OVERLAY_NONE = ""
private const val OVERLAY_SEND = "send"
private const val OVERLAY_SENT = "sent"

/**
 * The screen used while riding with a group: the map is the interface. Everything else is one glance away —
 * who is online, who lags behind, own speed, SOS and pause (docs/design-ui, "Màn đi đường").
 */
@Composable
fun RideScreen(tripId: String, onBack: () -> Unit) {
    val ctx = LocalContext.current
    val c = container()
    val vm = containerViewModel { RideViewModel(tripId, it) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    val working by vm.working.collectAsStateWithLifecycle()
    val msg by vm.messages.collectAsStateWithLifecycle()
    val invite by vm.invite.collectAsStateWithLifecycle()
    val inFlight by vm.sosInFlight.collectAsStateWithLifecycle()
    val snack = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var overlay by rememberSaveable { mutableStateOf(OVERLAY_NONE) }
    var sheetOpen by rememberSaveable { mutableStateOf(false) }
    var fitSignal by remember { mutableIntStateOf(0) }
    var confirmEnd by remember { mutableStateOf(false) }
    var confirmLeave by remember { mutableStateOf(false) }
    val acknowledged = remember { mutableStateListOf<String>() }
    val now by produceState(System.currentTimeMillis()) { while (true) { value = System.currentTimeMillis(); delay(5_000) } }

    LaunchedEffect(msg) { msg?.let { snack.showSnackbar(it); vm.messageShown() } }
    LaunchedEffect(Unit) { vm.sosOutcomes.collect { ok -> if (!ok) snack.showSnackbar("Không gửi được SOS. Hãy gọi 112 nếu nguy hiểm.") } }
    val flow = rememberPermissionFlow(onReady = { vm.startRecording { TrackingService.start(ctx) } })

    val live = ui.live
    val members = live?.members.orEmpty()
    val me = vm.me
    val tripStatus = live?.status?.ifEmpty { null } ?: ui.trip?.status
    val isOwner = ui.trip?.role == "owner"
    val myLatLng = ui.myPoint?.let { LatLng(it.lat, it.lng) }
        ?: members.firstOrNull { it.userId == me }?.location?.let { LatLng(it.lat, it.lng) }

    // An SOS from somebody else takes the whole screen until it is acknowledged (docs/design-ui, "Nhận SOS").
    val incoming = live?.sos.orEmpty().firstOrNull { it.status == "open" && it.senderId != me && it.sosId !in acknowledged }
    if (incoming != null) {
        LaunchedEffect(incoming.sosId) { vm.markSeen(incoming.sosId) }
        ReceiveSosPage(sos = incoming, me = myLatLng, onKnown = { acknowledged += incoming.sosId })
        return
    }
    if (overlay == OVERLAY_SEND) {
        SendSosPage(
            onPick = { r -> vm.sendSos(ctx, r.kind); overlay = OVERLAY_SENT },
            onCancel = { overlay = OVERLAY_NONE },
        )
        return
    }
    if (overlay == OVERLAY_SENT) {
        val state = when {
            inFlight -> SentState.SENDING
            ui.pendingSos > 0 -> if (ui.connection == ConnState.ONLINE) SentState.SENDING else SentState.OFFLINE
            else -> SentState.DELIVERED
        }
        SosSentPage(state, recipients = (members.size - 1).coerceAtLeast(0), onDone = { overlay = OVERLAY_NONE })
        return
    }

    val dark = isSystemInDarkTheme()
    val laggard = GroupWatch.laggard(members, me, myLatLng?.latitude, myLatLng?.longitude, now, ui.clockOffsetMs)
    val markers = members.mapIndexedNotNull { i, m ->
        val loc = m.location ?: return@mapIndexedNotNull null
        val self = m.userId == me
        val st = GroupWatch.statusOf(m, now, ui.clockOffsetMs)
        MapMarker(
            id = m.userId, at = LatLng(loc.lat, loc.lng), initial = m.name.take(1).uppercase(), color = parseHex(m.color, memberColor(i)),
            fade = when (st) { MemberStatus.LIVE -> 1f; MemberStatus.STALE -> 0.7f; else -> 0.45f },
            label = if (laggard?.member?.userId == m.userId) m.name else null, selfArrow = self,
        )
    }.let { list ->
        // own marker comes from the recording itself (the server never echoes my own position back)
        val own = myLatLng?.takeIf { list.none { it.id == me } }?.let { MapMarker(me ?: "me", it, "B", Color(0xFF1565C0), selfArrow = true) }
        list + listOfNotNull(own) + live?.sos.orEmpty().filter { it.status == "open" && it.lat != null && it.lng != null }
            .map { MapMarker("sos-" + it.sosId, LatLng(it.lat!!, it.lng!!), "!", Palette.Red, isSos = true, label = it.senderName) }
    }
    val mySos = live?.sos.orEmpty().firstOrNull { it.status == "open" && it.senderId == me }
    val online = GroupWatch.onlineCount(members)
    val ownKmh = ui.myPoint?.speedMps?.let { Math.round(it * 3.6).toInt() }
    val rec = ui.mySession

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        LiveMap(markers = markers, fitSignal = fitSignal, dark = dark)

        // ---- top: lag banner, group pill, "see everybody" ----
        Column(Modifier.fillMaxWidth()) {
            if (laggard != null) {
                Row(
                    Modifier.fillMaxWidth().background(Palette.Red).clickable { sheetOpen = true }.statusBarsPadding().heightIn(min = 65.dp).padding(horizontal = 14.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Box(Modifier.size(30.dp).background(Color.White, CircleShape), contentAlignment = Alignment.Center) {
                        Text("!", color = Palette.Red, fontSize = 20.sp, fontWeight = FontWeight.Black)
                    }
                    Text(GroupWatch.bannerText(laggard), color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.ExtraBold, modifier = Modifier.weight(1f))
                    Icon(Icons.Filled.KeyboardArrowRight, null, tint = Color.White, modifier = Modifier.size(28.dp))
                }
            }
            Row(
                Modifier.fillMaxWidth().let { if (laggard == null) it.statusBarsPadding() else it }.padding(start = 12.dp, end = 12.dp, top = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Surface(
                    onClick = { sheetOpen = true }, shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surface, shadowElevation = 4.dp,
                    modifier = Modifier.weight(1f).height(58.dp),
                ) {
                    Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        Box(Modifier.size(12.dp).background(if (ui.connection == ConnState.ONLINE) MaterialTheme.colorScheme.primary else Palette.Amber, CircleShape))
                        Text(
                            "$online/${members.size} người" + when (ui.connection) {
                                ConnState.ONLINE -> ""
                                ConnState.CONNECTING -> " · đang nối"
                                ConnState.SUPERSEDED -> " · mở ở máy khác"
                                ConnState.UPGRADE_REQUIRED -> " · cần cập nhật"
                                ConnState.OFFLINE -> " · mất mạng"
                            },
                            style = MaterialTheme.typography.titleMedium, maxLines = 1,
                        )
                    }
                }
                Surface(
                    onClick = { fitSignal++ }, shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surface, shadowElevation = 4.dp,
                    modifier = Modifier.height(58.dp),
                ) {
                    Row(Modifier.padding(horizontal = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Icon(Icons.Filled.Groups, null, Modifier.size(26.dp))
                        Text("Cả nhóm", style = MaterialTheme.typography.labelMedium.copy(fontSize = 16.sp))
                    }
                }
            }
            if (mySos != null) {
                Surface(
                    shape = RoundedCornerShape(14.dp), color = Palette.Red, modifier = Modifier.padding(start = 12.dp, end = 12.dp, top = 8.dp).fillMaxWidth(),
                ) {
                    Row(Modifier.padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            "SOS của bạn đang mở" + (mySos.delivery?.let { " · nhận ${it.received}/${it.total}" } ?: ""),
                            color = Color.White, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f),
                        )
                        TextButton(onClick = { vm.resolveSos(mySos.sosId) }) { Text("Tôi ổn", color = Color.White, fontWeight = FontWeight.Black) }
                    }
                }
            }
        }

        // ---- bottom: speed, SOS + pause, member sheet ----
        if (!sheetOpen) {
            Row(
                Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(start = 20.dp, end = 20.dp, bottom = 96.dp).navigationBarsPadding(),
                verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    if (rec != null) SpeedDial(ownKmh)
                    if (tripStatus == "active") SosFab(onClick = { overlay = OVERLAY_SEND })
                }
                RideMainButton(
                    tripStatus = tripStatus, recording = rec?.status, working = working,
                    onStart = { scope.launch { flow.begin(consented = c.prefs.hasLocationConsent()) } },
                    onPause = { TrackingService.pause(ctx) }, onResume = { TrackingService.resume(ctx) },
                    onOwnerStart = vm::startTrip,
                )
            }
        }
        MemberSheet(
            open = sheetOpen, onToggle = { sheetOpen = !sheetOpen }, title = live?.name?.ifEmpty { null } ?: ui.trip?.name ?: "Chuyến đi",
            members = members, me = me, online = online, laggardId = laggard?.member?.userId, now = now, offset = ui.clockOffsetMs,
            isOwner = isOwner, tripStatus = tripStatus, recording = rec != null, working = working,
            sharing = members.firstOrNull { it.userId == me }?.sharing ?: (ui.trip?.sharingState != "paused"),
            onSharing = vm::setSharing, onInvite = vm::createInvite, onEndTrip = { confirmEnd = true },
            onStopRecording = { TrackingService.end(ctx) }, onLeave = { confirmLeave = true }, onRemove = vm::remove, onBack = onBack,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
        SnackbarHost(snack, Modifier.align(Alignment.BottomCenter).padding(bottom = 100.dp))
    }

    invite?.let { code ->
        AlertDialog(
            onDismissRequest = vm::inviteShown,
            title = { Text("Mã mời của nhóm") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(code.chunked(4).joinToString(" "), fontSize = 40.sp, fontWeight = FontWeight.Black)
                    Text("Đọc mã này cho người muốn tham gia. Hiệu lực 24 giờ, tối đa 15 người.", style = MaterialTheme.typography.bodyMedium)
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                        .putExtra(Intent.EXTRA_TEXT, "Tham gia chuyến đi \"${ui.trip?.name.orEmpty()}\" trên MotoLink với mã: $code")
                    ctx.startActivity(Intent.createChooser(send, "Chia sẻ mã mời").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    vm.inviteShown()
                }) { Text("Chia sẻ", style = MaterialTheme.typography.titleMedium) }
            },
            dismissButton = { TextButton(onClick = vm::inviteShown) { Text("Đóng") } },
        )
    }
    if (confirmEnd) ConfirmDialog("Kết thúc chuyến đi?", "Mọi người sẽ ngừng chia sẻ vị trí.", onYes = { confirmEnd = false; vm.endTrip() }, onNo = { confirmEnd = false })
    if (confirmLeave) ConfirmDialog("Rời chuyến đi?", "Bạn sẽ không thấy vị trí của nhóm nữa. Trưởng đoàn cần chuyển quyền trước khi rời.", onYes = { confirmLeave = false; vm.leave(onBack) }, onNo = { confirmLeave = false })
}

/** The big green button bottom-right: start sharing, pause or resume, depending on where the ride is. */
@Composable
private fun RideMainButton(
    tripStatus: String?, recording: String?, working: Boolean,
    onStart: () -> Unit, onPause: () -> Unit, onResume: () -> Unit, onOwnerStart: () -> Unit,
) {
    val (label, icon, action) = when {
        tripStatus == "draft" -> Triple("Bắt đầu chuyến", Icons.Filled.PlayArrow, onOwnerStart)
        tripStatus != "active" -> return
        recording == null -> Triple("Bắt đầu", Icons.Filled.PlayArrow, onStart)
        recording == "paused" -> Triple("Tiếp tục", Icons.Filled.PlayArrow, onResume)
        else -> Triple("Tạm dừng", Icons.Filled.Pause, onPause)
    }
    Surface(
        onClick = action, enabled = !working, shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.primary,
        border = androidx.compose.foundation.BorderStroke(3.dp, Color.White), shadowElevation = 6.dp,
        modifier = Modifier.height(72.dp).heightIn(min = 72.dp),
    ) {
        Row(Modifier.padding(horizontal = 24.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(icon, null, Modifier.size(30.dp), tint = Color.White)
            Text(label, color = Color.White, style = MaterialTheme.typography.titleMedium)
        }
    }
}

@Composable
private fun MemberSheet(
    open: Boolean, onToggle: () -> Unit, title: String, members: List<MemberLive>, me: String?, online: Int, laggardId: String?,
    now: Long, offset: Long, isOwner: Boolean, tripStatus: String?, recording: Boolean, working: Boolean, sharing: Boolean,
    onSharing: (Boolean) -> Unit, onInvite: () -> Unit, onEndTrip: () -> Unit, onStopRecording: () -> Unit,
    onLeave: () -> Unit, onRemove: (String) -> Unit, onBack: () -> Unit, modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth().let { if (open) it.heightIn(max = 560.dp) else it.height(78.dp) },
        shape = RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp), color = MaterialTheme.colorScheme.surface, shadowElevation = 12.dp,
    ) {
        Column(Modifier.navigationBarsPadding()) {
            Column(
                Modifier.fillMaxWidth().clickable(onClick = onToggle).padding(top = 10.dp, bottom = 8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Box(Modifier.width(44.dp).height(5.dp).background(MaterialTheme.colorScheme.outline, RoundedCornerShape(3.dp)))
                if (!open) Text("$online đang trực tuyến", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = 10.dp))
            }
            if (open) {
                Row(Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(title, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f), maxLines = 1)
                    IconButton(onClick = onToggle, modifier = Modifier.size(56.dp)) { Icon(Icons.Filled.Close, "Đóng", Modifier.size(28.dp)) }
                }
                LazyColumn {
                    items(members, key = { it.userId }) { m ->
                        val st = GroupWatch.statusOf(m, now, offset)
                        val dim = st == MemberStatus.OFFLINE || st == MemberStatus.SHARING_HIDDEN
                        Row(
                            Modifier.fillMaxWidth().heightIn(min = 76.dp).padding(horizontal = 20.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp),
                        ) {
                            MemberAvatar(m.name.take(1).uppercase(), parseHex(m.color), size = 48.dp, alpha = if (dim) 0.5f else 1f)
                            Column(Modifier.weight(1f)) {
                                Text(m.name + if (m.userId == me) " (bạn)" else "", style = MaterialTheme.typography.bodyLarge)
                                Text(
                                    GroupWatch.rowText(m, now, offset) + if (m.recording) " · đang ghi" else "",
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = if (st == MemberStatus.LOST || st == MemberStatus.STALE) Palette.Red else MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            if (m.userId == laggardId) {
                                Text("Cần kiểm tra", color = Color.White, style = MaterialTheme.typography.labelMedium,
                                    modifier = Modifier.background(Palette.Red, RoundedCornerShape(10.dp)).padding(horizontal = 10.dp, vertical = 6.dp))
                            } else if (isOwner && m.userId != me) {
                                TextButton(onClick = { onRemove(m.userId) }) { Text("Mời ra", color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            }
                        }
                    }
                    item("sharing") {
                        SwitchRow("Hiện vị trí của tôi", "Tắt thì cả nhóm không thấy bạn", sharing, onSharing, enabled = !working)
                    }
                    if (isOwner) {
                        item("owner") {
                            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                if (tripStatus == "draft" || tripStatus == "active") SecondaryButton("Mời bạn bè", onInvite, enabled = !working)
                                if (tripStatus == "active") DangerButton("Kết thúc chuyến đi", onEndTrip, enabled = !working)
                            }
                        }
                    }
                    item("actions") {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                            if (recording) SecondaryButton("Dừng ghi hành trình", onStopRecording)
                            TextButton(onClick = onLeave, modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp)) {
                                Text("Rời chuyến đi", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            TextButton(onClick = onBack, modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp)) {
                                Text("Về danh sách chuyến", style = MaterialTheme.typography.bodyLarge)
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun ConfirmDialog(title: String, body: String, onYes: () -> Unit, onNo: () -> Unit) {
    AlertDialog(
        onDismissRequest = onNo, title = { Text(title) }, text = { Text(body) },
        confirmButton = { TextButton(onClick = onYes) { Text("Đồng ý", style = MaterialTheme.typography.titleMedium) } },
        dismissButton = { TextButton(onClick = onNo) { Text("Hủy", style = MaterialTheme.typography.titleMedium) } },
    )
}
