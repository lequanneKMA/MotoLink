package com.motolink.feature.trips

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MyLocation
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.motolink.AppContainer
import com.motolink.core.db.LocalSessionEntity
import com.motolink.core.db.TripEntity
import com.motolink.core.location.OneShotLocation
import com.motolink.core.net.ApiResult
import com.motolink.core.net.userMessage
import com.motolink.feature.map.LiveMap
import com.motolink.feature.map.MapMarker
import com.motolink.feature.notifications.AppNotifications
import com.motolink.feature.realtime.ConnState
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.hasLocationConsent
import com.motolink.feature.tracking.rememberPermissionFlow
import com.motolink.logic.JwtExpiry
import com.motolink.logic.MemberLive
import com.motolink.logic.MemberStatus
import com.motolink.logic.PresenceClassifier
import com.motolink.logic.SosAlert
import com.motolink.logic.TripLiveState
import com.motolink.ui.container
import com.motolink.ui.containerViewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import org.maplibre.android.geometry.LatLng

data class DetailUi(
    val trip: TripEntity? = null,
    val live: TripLiveState? = null,
    val connection: ConnState = ConnState.OFFLINE,
    val mySession: LocalSessionEntity? = null,
    val pendingSos: Int = 0,
    val clockOffsetMs: Long = 0,
)

class TripDetailViewModel(private val tripId: String, private val c: AppContainer) : ViewModel() {
    private val message = MutableStateFlow<String?>(null)
    val messages: StateFlow<String?> = message
    private val busy = MutableStateFlow(false)
    val working: StateFlow<Boolean> = busy
    private val _invite = MutableStateFlow<String?>(null)
    val invite: StateFlow<String?> = _invite

    val ui: StateFlow<DetailUi> = combine(
        c.trips.trip(tripId), c.realtime.live(tripId), c.realtime.connection, c.tracking.openSession, c.sos.pendingCount, c.realtime.clockOffsetMs,
    ) { values ->
        DetailUi(
            trip = values[0] as TripEntity?, live = values[1] as TripLiveState?, connection = values[2] as ConnState,
            mySession = (values[3] as LocalSessionEntity?)?.takeIf { it.tripId == tripId },
            pendingSos = values[4] as Int, clockOffsetMs = values[5] as Long,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), DetailUi())

    val sosOutcomes: Flow<String> = kotlinx.coroutines.flow.flow {
        c.realtime.sosOutcome.collect { o ->
            if (o.tripId == tripId) emit(if (o.ok) "SOS đã được gửi tới cả nhóm." else "Không gửi được SOS (${o.code}).")
        }
    }

    val me: String? get() = c.tokens.current()?.access?.let { JwtExpiry.subject(it) }

    init {
        c.realtime.watch(tripId)
        viewModelScope.launch { c.trips.refresh() }
    }

    override fun onCleared() {
        c.realtime.unwatch(tripId)
    }

    fun messageShown() {
        message.value = null
    }

    private fun work(block: suspend () -> Unit) {
        if (busy.value) return
        busy.value = true
        viewModelScope.launch {
            try {
                block()
            } finally {
                busy.value = false
            }
        }
    }

    private fun <T> ApiResult<T>.report(): Boolean = when (this) {
        is ApiResult.Ok -> true
        is ApiResult.Err -> { message.value = failure.userMessage(); false }
    }

    fun startTrip() = work { c.trips.start(tripId).report() }
    fun endTrip() = work { c.trips.end(tripId).report() }
    fun leave(onLeft: () -> Unit) = work { if (c.trips.leave(tripId).report()) onLeft() }
    fun setSharing(on: Boolean) = work { c.trips.setSharing(tripId, on).report() }
    fun remove(userId: String) = work { c.trips.remove(tripId, userId).report() }

    fun createInvite() = work {
        val r = c.trips.invite(tripId)
        if (r.report()) _invite.value = (r as ApiResult.Ok).value.code
    }

    fun inviteShown() {
        _invite.value = null
    }

    /** Creates the server session; the caller starts the foreground service when this returns true. */
    fun startRecording(onReady: () -> Unit) = work {
        // The server requires the processing consent before any session (docs/design/11 §5.1).
        if (!c.trips.grantLocationConsent().report()) return@work
        if (c.trips.startSession(tripId).report()) onReady()
    }

    fun sendSos(ctx: android.content.Context, kind: String, text: String) {
        viewModelScope.launch {
            val fix = OneShotLocation.get(ctx)
            c.sos.raise(tripId, kind, text, fix?.lat, fix?.lng, fix?.accuracyM)
            message.value = "Đang gửi SOS…"
        }
    }

    fun markSeen(sosId: String) {
        c.realtime.sendReceipt(tripId, sosId, "seen")
        viewModelScope.launch { c.sos.receipt(sosId, "seen") }
    }

    fun resolveSos(sosId: String) = work { c.sos.resolve(sosId, "resolved").report() }
}

private fun parseColor(hex: String?, fallback: String = "#546E7A"): Color =
    runCatching { Color(android.graphics.Color.parseColor(hex ?: fallback)) }.getOrDefault(Color(0xFF546E7A))

private fun statusText(s: MemberStatus, ageS: Long?): String = when (s) {
    MemberStatus.SHARING_HIDDEN -> "Đã tắt chia sẻ vị trí"
    MemberStatus.OFFLINE -> "Ngoại tuyến"
    MemberStatus.LIVE -> "Trực tiếp"
    MemberStatus.STALE -> "Vị trí cũ" + (ageS?.let { " (${it}s trước)" } ?: "")
    MemberStatus.LOST -> "Mất tín hiệu" + (ageS?.let { " (${it / 60} phút)" } ?: "")
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TripDetailScreen(tripId: String, onBack: () -> Unit) {
    val ctx = LocalContext.current
    val c = container()
    val vm = containerViewModel { TripDetailViewModel(tripId, it) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    val working by vm.working.collectAsStateWithLifecycle()
    val msg by vm.messages.collectAsStateWithLifecycle()
    val invite by vm.invite.collectAsStateWithLifecycle()
    val snack = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var fitSignal by remember { mutableIntStateOf(0) }
    var confirmEnd by remember { mutableStateOf(false) }
    var confirmLeave by remember { mutableStateOf(false) }
    var showSos by remember { mutableStateOf(false) }

    LaunchedEffect(msg) { msg?.let { snack.showSnackbar(it); vm.messageShown() } }
    LaunchedEffect(Unit) { vm.sosOutcomes.collect { snack.showSnackbar(it) } }
    val now by produceState(System.currentTimeMillis()) { while (true) { value = System.currentTimeMillis(); delay(5_000) } }

    val flow = rememberPermissionFlow(onReady = { vm.startRecording { TrackingService.start(ctx) } })

    val trip = ui.trip
    val live = ui.live
    val isOwner = trip?.role == "owner"
    val me = vm.me

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(live?.name?.ifEmpty { null } ?: trip?.name ?: "Chuyến đi", maxLines = 1)
                        Text(
                            (trip?.status?.let { statusLabel(live?.status?.ifEmpty { null } ?: it) } ?: "") +
                                when (ui.connection) {
                                    ConnState.ONLINE -> ""
                                    ConnState.CONNECTING -> " · đang kết nối…"
                                    ConnState.SUPERSEDED -> " · mở ở thiết bị khác"
                                    ConnState.UPGRADE_REQUIRED -> " · cần cập nhật ứng dụng"
                                    ConnState.OFFLINE -> " · ngoại tuyến"
                                },
                            style = MaterialTheme.typography.labelSmall,
                        )
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Quay lại") } },
                actions = { IconButton(onClick = { fitSignal++ }) { Icon(Icons.Filled.MyLocation, "Xem cả nhóm") } },
            )
        },
        snackbarHost = { SnackbarHost(snack) },
    ) { pad ->
        val members = live?.members.orEmpty()
        val markers = members.mapNotNull { m ->
            val loc = m.location ?: return@mapNotNull null
            val st = PresenceClassifier.classify(m.online, m.sharing, loc.serverTs, now, ui.clockOffsetMs)
            MapMarker(m.userId, LatLng(loc.lat, loc.lng), m.color ?: "#546E7A", fade = if (st == MemberStatus.LIVE) 1f else if (st == MemberStatus.STALE) 0.6f else 0.3f)
        } + live?.sos.orEmpty().filter { it.status == "open" && it.lat != null && it.lng != null }
            .map { MapMarker("sos-" + it.sosId, LatLng(it.lat!!, it.lng!!), "#D50000", 1f, isSos = true) }

        Column(Modifier.padding(pad).fillMaxSize()) {
            Box(Modifier.fillMaxWidth().weight(0.9f)) { LiveMap(markers, fitSignal) }
            LazyColumn(
                Modifier.fillMaxWidth().weight(1.3f),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                // ---- SOS alerts first: they matter more than anything else on this screen ----
                items(live?.sos.orEmpty().filter { it.status == "open" }, key = { "sos-" + it.sosId }) { a ->
                    SosCard(a, mine = a.senderId == me, canResolve = isOwner || a.senderId == me, onSeen = { vm.markSeen(a.sosId) }, onResolve = { vm.resolveSos(a.sosId) })
                }
                // ---- recording & trip controls ----
                item("controls") {
                    Controls(
                        ui = ui, isOwner = isOwner, working = working,
                        onStartTrip = vm::startTrip,
                        onEndTrip = { confirmEnd = true },
                        onInvite = vm::createInvite,
                        onStartRecording = { scope.launch { flow.begin(consented = c.prefs.hasLocationConsent()) } },
                        onPause = { TrackingService.pause(ctx) },
                        onResume = { TrackingService.resume(ctx) },
                        onStopRecording = { TrackingService.end(ctx) },
                    )
                }
                item("sharing") {
                    val sharing = (members.firstOrNull { it.userId == me }?.sharing) ?: (trip?.sharingState != "paused")
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text("Hiện vị trí của tôi với nhóm", Modifier.weight(1f))
                        Switch(checked = sharing, onCheckedChange = { vm.setSharing(it) }, enabled = !working)
                    }
                }
                item("members-title") { Text("Thành viên (${members.size})", style = MaterialTheme.typography.titleMedium) }
                items(members, key = { it.userId }) { m ->
                    MemberRow(m, mine = m.userId == me, now = now, offset = ui.clockOffsetMs, canRemove = isOwner && m.userId != me, onRemove = { vm.remove(m.userId) })
                }
                item("sos-btn") {
                    Button(
                        onClick = { showSos = true },
                        enabled = trip?.status == "active",
                        colors = ButtonDefaults.buttonColors(containerColor = Color(0xFFD50000)),
                        modifier = Modifier.fillMaxWidth().height(56.dp),
                    ) { Text("SOS — Cần giúp đỡ") }
                    if (ui.pendingSos > 0) Text("Có ${ui.pendingSos} SOS đang chờ gửi…", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
                item("leave") {
                    TextButton(onClick = { confirmLeave = true }, modifier = Modifier.fillMaxWidth()) { Text("Rời chuyến đi") }
                }
            }
        }
    }

    if (showSos) SosDialog(onDismiss = { showSos = false }, onSend = { kind, text -> showSos = false; vm.sendSos(ctx, kind, text) })
    invite?.let { code ->
        AlertDialog(
            onDismissRequest = vm::inviteShown,
            title = { Text("Mã mời") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(code, style = MaterialTheme.typography.headlineMedium)
                    Text("Hiệu lực 24 giờ, tối đa 15 người. Chỉ gửi cho người bạn tin tưởng.", style = MaterialTheme.typography.bodySmall)
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                        .putExtra(Intent.EXTRA_TEXT, "Tham gia chuyến đi \"${trip?.name.orEmpty()}\" trên MotoLink với mã: $code")
                    ctx.startActivity(Intent.createChooser(send, "Chia sẻ mã mời").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    vm.inviteShown()
                }) { Text("Chia sẻ") }
            },
            dismissButton = { TextButton(onClick = vm::inviteShown) { Text("Đóng") } },
        )
    }
    if (confirmEnd) ConfirmDialog("Kết thúc chuyến đi?", "Mọi người sẽ ngừng chia sẻ vị trí.", onYes = { confirmEnd = false; vm.endTrip() }, onNo = { confirmEnd = false })
    if (confirmLeave) ConfirmDialog("Rời chuyến đi?", "Bạn sẽ không thấy vị trí của nhóm nữa. Trưởng đoàn cần chuyển quyền trước khi rời.", onYes = { confirmLeave = false; vm.leave(onBack) }, onNo = { confirmLeave = false })
}

@Composable
private fun Controls(
    ui: DetailUi, isOwner: Boolean, working: Boolean,
    onStartTrip: () -> Unit, onEndTrip: () -> Unit, onInvite: () -> Unit,
    onStartRecording: () -> Unit, onPause: () -> Unit, onResume: () -> Unit, onStopRecording: () -> Unit,
) {
    val status = ui.live?.status?.ifEmpty { null } ?: ui.trip?.status
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (isOwner) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                when (status) {
                    "draft" -> Button(onClick = onStartTrip, enabled = !working, modifier = Modifier.weight(1f)) { Text("Bắt đầu chuyến đi") }
                    "active" -> OutlinedButton(onClick = onEndTrip, enabled = !working, modifier = Modifier.weight(1f)) { Text("Kết thúc chuyến đi") }
                }
                if (status == "draft" || status == "active") OutlinedButton(onClick = onInvite, enabled = !working, modifier = Modifier.weight(1f)) { Text("Mời bạn bè") }
            }
        }
        if (status == "active") {
            val s = ui.mySession
            when {
                s == null -> Button(onClick = onStartRecording, enabled = !working, modifier = Modifier.fillMaxWidth()) { Text("Bắt đầu ghi & chia sẻ vị trí") }
                s.status == "paused" -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = onResume, modifier = Modifier.weight(1f)) { Text("Tiếp tục") }
                    OutlinedButton(onClick = onStopRecording, modifier = Modifier.weight(1f)) { Text("Dừng ghi") }
                }
                else -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onPause, modifier = Modifier.weight(1f)) { Text("Tạm dừng") }
                    Button(onClick = onStopRecording, modifier = Modifier.weight(1f)) { Text("Dừng ghi") }
                }
            }
        } else if (status == "draft") {
            Text("Trưởng đoàn bắt đầu chuyến đi thì mọi người mới có thể chia sẻ vị trí.", style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun MemberRow(m: MemberLive, mine: Boolean, now: Long, offset: Long, canRemove: Boolean, onRemove: () -> Unit) {
    val st = PresenceClassifier.classify(m.online, m.sharing, m.location?.serverTs, now, offset)
    val age = m.location?.let { (now + offset - it.serverTs) / 1000 }
    var confirm by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(14.dp).background(parseColor(m.color), CircleShape))
        Column(Modifier.weight(1f)) {
            Text(m.name + (if (mine) " (bạn)" else "") + (if (m.role == "owner") " · trưởng đoàn" else ""), style = MaterialTheme.typography.bodyLarge)
            Text(
                statusText(st, age) + if (m.recording) " · đang ghi" else "",
                style = MaterialTheme.typography.bodySmall,
                color = when (st) {
                    MemberStatus.LIVE -> MaterialTheme.colorScheme.primary
                    MemberStatus.STALE, MemberStatus.LOST -> MaterialTheme.colorScheme.error
                    else -> MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
        }
        if (canRemove) TextButton(onClick = { confirm = true }) { Text("Mời ra") }
    }
    if (confirm) ConfirmDialog("Mời ${m.name} ra khỏi chuyến đi?", "Họ sẽ không thấy vị trí của nhóm nữa.", onYes = { confirm = false; onRemove() }, onNo = { confirm = false })
}

@Composable
private fun SosCard(a: SosAlert, mine: Boolean, canResolve: Boolean, onSeen: () -> Unit, onResolve: () -> Unit) {
    // Opening the card counts as "seen" (once per alert).
    if (!mine) LaunchedEffect(a.sosId) { onSeen() }
    Card(colors = CardDefaults.cardColors(containerColor = Color(0xFFFFEBEE)), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                "SOS: " + (if (mine) "Bạn" else a.senderName ?: "Một thành viên") + " " + AppNotifications.kindLabel(a.kind),
                style = MaterialTheme.typography.titleMedium, color = Color(0xFFB71C1C),
            )
            a.message?.let { Text(it) }
            if (a.lat == null) Text("Không có vị trí kèm theo.", style = MaterialTheme.typography.bodySmall)
            a.delivery?.let { Text("Đã nhận ${it.received}/${it.total} · đã xem ${it.seen}", style = MaterialTheme.typography.bodySmall) }
            if (canResolve) OutlinedButton(onClick = onResolve) { Text(if (mine) "Tôi đã an toàn" else "Đánh dấu đã xử lý") }
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun SosDialog(onDismiss: () -> Unit, onSend: (String, String) -> Unit) {
    var kind by remember { mutableStateOf("general") }
    var text by remember { mutableStateOf("") }
    val kinds = listOf("breakdown" to "Hỏng xe", "accident" to "Tai nạn", "lost" to "Bị lạc", "medical" to "Cần y tế", "general" to "Khác")
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Gửi SOS tới cả nhóm?") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Mọi thành viên sẽ nhận cảnh báo kèm vị trí hiện tại của bạn.", style = MaterialTheme.typography.bodySmall)
                androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    kinds.forEach { (k, label) -> FilterChip(selected = kind == k, onClick = { kind = k }, label = { Text(label) }) }
                }
                OutlinedTextField(text, { text = it.take(280) }, label = { Text("Ghi chú (không bắt buộc)") })
            }
        },
        confirmButton = { Button(onClick = { onSend(kind, text) }, colors = ButtonDefaults.buttonColors(containerColor = Color(0xFFD50000))) { Text("Gửi SOS") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Hủy") } },
    )
}

@Composable
private fun ConfirmDialog(title: String, body: String, onYes: () -> Unit, onNo: () -> Unit) {
    AlertDialog(
        onDismissRequest = onNo, title = { Text(title) }, text = { Text(body) },
        confirmButton = { TextButton(onClick = onYes) { Text("Đồng ý") } },
        dismissButton = { TextButton(onClick = onNo) { Text("Hủy") } },
    )
}

