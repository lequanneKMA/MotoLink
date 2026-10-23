package com.motolink.feature.trips

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.motolink.core.db.TripEntity
import com.motolink.core.net.ApiResult
import com.motolink.core.net.userMessage
import com.motolink.ui.CenteredHint
import com.motolink.ui.LabeledField
import com.motolink.ui.PageHeader
import com.motolink.ui.PrimaryButton
import com.motolink.ui.SecondaryButton
import com.motolink.ui.containerViewModel
import com.motolink.ui.memberColor
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class TripsUi(val trips: List<TripEntity> = emptyList(), val busy: Boolean = false, val message: String? = null, val created: Created? = null)

/** A trip the user just created, with its invitation code ready to read out. */
data class Created(val tripId: String, val code: String?)

class TripsViewModel(private val repo: TripRepository) : ViewModel() {
    private val busy = MutableStateFlow(false)
    private val message = MutableStateFlow<String?>(null)
    private val created = MutableStateFlow<Created?>(null)

    val ui: StateFlow<TripsUi> = combine(repo.trips, busy, message, created) { t, b, m, c -> TripsUi(t, b, m, c) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), TripsUi())

    init {
        refresh()
    }

    fun refresh() = launchBusy {
        (repo.refresh() as? ApiResult.Err)?.let { message.value = it.failure.userMessage() }
    }

    /** Creates the trip and its first invitation so the code can be shown right away. */
    fun create(name: String) = launchBusy {
        when (val r = repo.create(name)) {
            is ApiResult.Ok -> {
                val code = (repo.invite(r.value.id) as? ApiResult.Ok)?.value?.code
                created.value = Created(r.value.id, code)
            }
            is ApiResult.Err -> message.value = r.failure.userMessage()
        }
    }

    fun join(code: String, onJoined: (String) -> Unit) = launchBusy {
        when (val r = repo.join(code)) {
            is ApiResult.Ok -> onJoined(r.value.id)
            is ApiResult.Err -> message.value = r.failure.userMessage()
        }
    }

    fun messageShown() = message.update { null }
    fun createdShown() = created.update { null }

    private fun launchBusy(block: suspend () -> Unit) {
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
}

fun statusLabel(status: String) = when (status) {
    "draft" -> "Chưa bắt đầu"
    "active" -> "Đang đi"
    "ended" -> "Đã kết thúc"
    "archived" -> "Đã lưu trữ"
    else -> status
}

/** "Chuyến đi": a plain list like a chat list — one trip per row, a round "+" for new/join. */
@Composable
fun TripsScreen(onOpen: (String) -> Unit, onNew: () -> Unit, onSettings: () -> Unit, signedIn: Boolean, onLogin: () -> Unit) {
    val vm = containerViewModel { TripsViewModel(it.trips) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    val snack = remember { SnackbarHostState() }
    LaunchedEffect(ui.message) { ui.message?.let { snack.showSnackbar(it); vm.messageShown() } }

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        Column(Modifier.fillMaxSize()) {
            Row(Modifier.fillMaxWidth().statusBarsPadding().padding(start = 20.dp, end = 8.dp, top = 12.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("MOTOLINK", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, letterSpacing = 1.2.sp)
                    Text("Chuyến đi", style = MaterialTheme.typography.headlineMedium)
                }
                IconButton(onClick = onSettings, modifier = Modifier.size(56.dp)) { Icon(Icons.Filled.Settings, "Cài đặt", Modifier.size(30.dp)) }
            }
            when {
                !signedIn -> Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Text("Đăng nhập để tạo chuyến đi và xem vị trí của bạn bè.", style = MaterialTheme.typography.bodyLarge)
                    PrimaryButton("Đăng nhập", onLogin)
                }
                ui.trips.isEmpty() -> CenteredHint("Chưa có chuyến đi nào.\nChạm + để tạo chuyến hoặc nhập mã mời.")
                else -> LazyColumn(Modifier.weight(1f)) {
                    items(ui.trips, key = { it.tripId }) { t -> TripRow(t, index = ui.trips.indexOf(t), onClick = { onOpen(t.tripId) }) }
                }
            }
        }
        if (signedIn) {
            Surface(
                onClick = onNew, shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.primary, shadowElevation = 8.dp,
                modifier = Modifier.align(Alignment.BottomEnd).padding(20.dp).navigationBarsPadding().size(68.dp),
            ) { Box(contentAlignment = Alignment.Center) { Icon(Icons.Filled.Add, "Tạo hoặc tham gia chuyến đi", Modifier.size(34.dp), tint = Color.White) } }
        }
        SnackbarHost(snack, Modifier.align(Alignment.BottomCenter).padding(bottom = 100.dp))
    }
}

@Composable
private fun TripRow(t: TripEntity, index: Int, onClick: () -> Unit) {
    Column {
        Row(
            Modifier.fillMaxWidth().heightIn(min = 84.dp).clickable(onClick = onClick).padding(horizontal = 20.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Box(Modifier.size(52.dp).background(memberColor(index + 1), RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                Text(t.name.take(1).uppercase(), color = Color.White, fontSize = 24.sp, fontWeight = FontWeight.Black)
            }
            Column(Modifier.weight(1f)) {
                Text(t.name, style = MaterialTheme.typography.titleMedium, maxLines = 1)
                Text(
                    statusLabel(t.status) + if (t.role == "owner") " · trưởng đoàn" else "",
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (t.status == "active") MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Icon(Icons.Filled.KeyboardArrowRight, null, Modifier.size(28.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        HorizontalDivider(color = MaterialTheme.colorScheme.outline)
    }
}

/** "Thêm chuyến đi": create a trip (then read out its code) or join with a code. Two fields, two buttons. */
@Composable
fun CreateTripScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val ctx = LocalContext.current
    val vm = containerViewModel { TripsViewModel(it.trips) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    var name by rememberSaveable { mutableStateOf("") }
    var code by rememberSaveable { mutableStateOf("") }
    val snack = remember { SnackbarHostState() }
    LaunchedEffect(ui.message) { ui.message?.let { snack.showSnackbar(it); vm.messageShown() } }
    val created = ui.created

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        Column(Modifier.fillMaxSize().navigationBarsPadding()) {
            PageHeader(if (created == null) "Thêm chuyến đi" else "Đã tạo chuyến", onBack = onBack)
            if (created == null) {
                Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    LabeledField("Tên chuyến đi", name, { name = it.take(80) }, placeholder = "Ví dụ: Đà Lạt cuối tuần")
                    PrimaryButton("Tạo chuyến mới", { vm.create(name) }, enabled = name.trim().length >= 2 && !ui.busy)
                    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        HorizontalDivider(Modifier.weight(1f), color = MaterialTheme.colorScheme.outline)
                        Text("hoặc", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        HorizontalDivider(Modifier.weight(1f), color = MaterialTheme.colorScheme.outline)
                    }
                    LabeledField("Mã mời", code, { code = it.uppercase().filter { ch -> ch.isLetterOrDigit() }.take(16) }, placeholder = "Nhập mã được gửi", mono = true)
                    SecondaryButton("Tham gia chuyến", { vm.join(code) { id -> onOpen(id) } }, enabled = code.length >= 6 && !ui.busy)
                }
            } else {
                Column(
                    Modifier.fillMaxSize().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(14.dp, Alignment.CenterVertically),
                ) {
                    Text("MÃ MỜI CỦA NHÓM", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.2.sp)
                    Text(
                        created.code?.chunked(4)?.joinToString(" ") ?: "—", fontSize = 48.sp, fontWeight = FontWeight.Black, textAlign = TextAlign.Center,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Text("Đọc mã này cho người muốn tham gia", style = MaterialTheme.typography.bodyLarge, textAlign = TextAlign.Center)
                    if (created.code != null) {
                        PrimaryButton("Chia sẻ", {
                            val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                                .putExtra(Intent.EXTRA_TEXT, "Tham gia chuyến đi trên MotoLink với mã: ${created.code}")
                            ctx.startActivity(Intent.createChooser(send, "Chia sẻ mã mời").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                        }, icon = Icons.Filled.Share)
                    }
                    SecondaryButton("Vào bản đồ nhóm", { vm.createdShown(); onOpen(created.tripId) })
                }
            }
        }
        SnackbarHost(snack, Modifier.align(Alignment.BottomCenter).padding(bottom = 24.dp))
    }
}
