package com.motolink.feature.trips

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.motolink.core.db.TripEntity
import com.motolink.core.net.ApiResult
import com.motolink.core.net.userMessage
import com.motolink.ui.containerViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class TripsUi(val trips: List<TripEntity> = emptyList(), val busy: Boolean = false, val message: String? = null)

class TripsViewModel(private val repo: TripRepository) : ViewModel() {
    private val busy = MutableStateFlow(false)
    private val message = MutableStateFlow<String?>(null)

    val ui: StateFlow<TripsUi> = combine(repo.trips, busy, message) { t, b, m -> TripsUi(t, b, m) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), TripsUi())

    init {
        refresh()
    }

    fun refresh() = launchBusy {
        (repo.refresh() as? ApiResult.Err)?.let { message.value = it.failure.userMessage() }
    }

    fun create(name: String, onCreated: (String) -> Unit) = launchBusy {
        when (val r = repo.create(name)) {
            is ApiResult.Ok -> onCreated(r.value.id)
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
    "active" -> "Đang diễn ra"
    "ended" -> "Đã kết thúc"
    "archived" -> "Đã lưu trữ"
    else -> status
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TripsScreen(onBack: () -> Unit, onOpen: (String) -> Unit, signedIn: Boolean, onLogin: () -> Unit) {
    val vm = containerViewModel { TripsViewModel(it.trips) }
    val ui by vm.ui.collectAsStateWithLifecycle()
    var showCreate by remember { mutableStateOf(false) }
    var showJoin by remember { mutableStateOf(false) }
    val snack = remember { SnackbarHostState() }
    LaunchedEffect(ui.message) {
        ui.message?.let {
            snack.showSnackbar(it)
            vm.messageShown()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Chuyến đi") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Quay lại") } },
                actions = { IconButton(onClick = vm::refresh, enabled = !ui.busy && signedIn) { Icon(Icons.Filled.Refresh, "Làm mới") } },
            )
        },
        snackbarHost = { SnackbarHost(snack) },
    ) { pad ->
        if (!signedIn) {
            Column(Modifier.padding(pad).padding(24.dp).fillMaxSize(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("Đăng nhập để tạo chuyến đi và xem vị trí của bạn bè.", style = MaterialTheme.typography.bodyLarge)
                Button(onClick = onLogin, modifier = Modifier.fillMaxWidth()) { Text("Đăng nhập") }
            }
            return@Scaffold
        }
        Column(Modifier.padding(pad).fillMaxSize()) {
            Row(Modifier.fillMaxWidth().padding(12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { showCreate = true }, modifier = Modifier.weight(1f)) {
                    Icon(Icons.Filled.Add, null)
                    Text("Tạo chuyến đi", Modifier.padding(start = 8.dp))
                }
                OutlinedButton(onClick = { showJoin = true }, modifier = Modifier.weight(1f)) { Text("Nhập mã mời") }
            }
            if (ui.trips.isEmpty()) {
                Text("Chưa có chuyến đi nào. Tạo một chuyến hoặc nhập mã từ bạn bè.", Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
            LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(ui.trips, key = { it.tripId }) { t ->
                    Card(Modifier.fillMaxWidth().clickable { onOpen(t.tripId) }) {
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(t.name, style = MaterialTheme.typography.titleMedium)
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                AssistChip(onClick = { onOpen(t.tripId) }, label = { Text(statusLabel(t.status)) })
                                if (t.role == "owner") AssistChip(onClick = { onOpen(t.tripId) }, label = { Text("Trưởng đoàn") })
                            }
                        }
                    }
                }
            }
        }
    }

    if (showCreate) {
        var name by remember { mutableStateOf("") }
        AlertDialog(
            onDismissRequest = { showCreate = false },
            title = { Text("Tạo chuyến đi") },
            text = {
                OutlinedTextField(name, { name = it.take(80) }, label = { Text("Tên chuyến đi") }, singleLine = true)
            },
            confirmButton = {
                TextButton(
                    enabled = name.trim().length >= 2 && !ui.busy,
                    onClick = { vm.create(name) { id -> showCreate = false; onOpen(id) } },
                ) { Text("Tạo") }
            },
            dismissButton = { TextButton(onClick = { showCreate = false }) { Text("Hủy") } },
        )
    }
    if (showJoin) {
        var code by remember { mutableStateOf("") }
        AlertDialog(
            onDismissRequest = { showJoin = false },
            title = { Text("Nhập mã mời") },
            text = {
                OutlinedTextField(
                    code, { code = it.uppercase().take(16) }, label = { Text("Mã mời") }, singleLine = true,
                    keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(capitalization = KeyboardCapitalization.Characters),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = code.trim().length >= 6 && !ui.busy,
                    onClick = { vm.join(code) { id -> showJoin = false; onOpen(id) } },
                ) { Text("Tham gia") }
            },
            dismissButton = { TextButton(onClick = { showJoin = false }) { Text("Hủy") } },
        )
    }
}
