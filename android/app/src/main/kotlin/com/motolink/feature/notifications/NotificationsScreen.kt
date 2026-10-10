package com.motolink.feature.notifications

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.motolink.core.net.ApiResult
import com.motolink.core.net.NotificationDto
import com.motolink.core.net.ReadNotificationsRequest
import com.motolink.core.net.apiCall
import com.motolink.ui.container
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/** In-app inbox (GET /notifications): the history of what push told the user, readable without FCM. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NotificationsScreen(onBack: () -> Unit, onOpenTrip: (String) -> Unit) {
    val api = container().network.api
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<NotificationDto>?>(null) }

    LaunchedEffect(Unit) {
        items = (apiCall { api.notifications().items } as? ApiResult.Ok)?.value ?: emptyList()
    }

    Scaffold(topBar = {
        TopAppBar(
            title = { Text("Thông báo") },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Quay lại") } },
            actions = {
                TextButton(onClick = {
                    scope.launch {
                        apiCall { api.readNotifications(ReadNotificationsRequest(all = true)) }
                        items = items?.map { it.copy(readAt = it.readAt ?: "now") }
                    }
                }) { Text("Đánh dấu đã đọc") }
            },
        )
    }) { pad ->
        val list = items
        when {
            list == null -> Text("Đang tải…", Modifier.padding(pad).padding(16.dp))
            list.isEmpty() -> Text("Chưa có thông báo.", Modifier.padding(pad).padding(16.dp))
            else -> LazyColumn(Modifier.padding(pad).fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(list, key = { it.id }) { n ->
                    val title = (n.payload?.get("title") as? JsonPrimitive)?.contentOrNull ?: n.type
                    val body = (n.payload?.get("body") as? JsonPrimitive)?.contentOrNull.orEmpty()
                    Card(Modifier.fillMaxWidth().clickable(enabled = n.tripId != null) { n.tripId?.let(onOpenTrip) }) {
                        Column(Modifier.padding(14.dp)) {
                            Text(title, style = MaterialTheme.typography.titleSmall, color = if (n.readAt == null) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
                            if (body.isNotEmpty()) Text(body, style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                }
            }
        }
    }
}
