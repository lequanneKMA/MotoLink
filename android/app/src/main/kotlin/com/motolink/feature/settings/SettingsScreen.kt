package com.motolink.feature.settings

import android.os.Build
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
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
import com.motolink.core.location.LocationPermissions
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.openAppSettings
import com.motolink.logic.OemGuide
import com.motolink.ui.container
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(onBack: () -> Unit, onLogin: () -> Unit) {
    val c = container()
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val signedIn by c.auth.signedIn.collectAsStateWithLifecycle(initialValue = false)
    val perms = LocationPermissions.read(ctx)
    val guide = OemGuide.forManufacturer(Build.MANUFACTURER)

    Scaffold(topBar = {
        TopAppBar(
            title = { Text(stringResource(R.string.settings_title)) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, null) } },
        )
    }) { pad ->
        Column(
            Modifier.padding(pad).padding(16.dp).fillMaxSize().verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Tài khoản", style = MaterialTheme.typography.titleMedium)
            if (signedIn) {
                OutlinedButton(onClick = { scope.launch { c.signOut() } }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.logout)) }
            } else {
                Button(onClick = onLogin, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.action_login)) }
            }
            HorizontalDivider()

            Text("Quyền riêng tư và vị trí", style = MaterialTheme.typography.titleMedium)
            Text("Vị trí: " + if (perms.fine) "chính xác" else if (perms.coarse) "xấp xỉ" else "chưa cấp")
            if (perms.approximateOnly) Text(stringResource(R.string.perm_approximate_warning), color = MaterialTheme.colorScheme.error)
            Text("Vị trí nền (khi tắt màn hình): " + if (perms.background) "đã cho phép" else "chưa cho phép")
            OutlinedButton(onClick = { openAppSettings(ctx) }, modifier = Modifier.fillMaxWidth()) { Text("Mở cài đặt quyền của ứng dụng") }
            OutlinedButton(
                onClick = {
                    // Withdrawing consent stops recording immediately (docs/design/11 §5.1).
                    TrackingService.end(ctx)
                    scope.launch {
                        c.prefs.withdrawLocationConsent()
                        c.trips.withdrawLocationConsent()
                    }
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Rút lại đồng ý dùng vị trí") }
            HorizontalDivider()

            Text(stringResource(R.string.oem_title), style = MaterialTheme.typography.titleMedium)
            Text(guide.brand, style = MaterialTheme.typography.labelLarge)
            guide.steps.forEach { Text("• $it", style = MaterialTheme.typography.bodyMedium) }
            Text(
                "Các bước có thể khác tùy phiên bản hệ điều hành. Đây chỉ làm giảm khả năng bị tắt ngầm, không đảm bảo tuyệt đối.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}
