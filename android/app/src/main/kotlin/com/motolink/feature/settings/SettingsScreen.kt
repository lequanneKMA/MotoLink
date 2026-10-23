package com.motolink.feature.settings

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.core.location.LocationPermissions
import com.motolink.feature.tracking.TrackingService
import com.motolink.feature.tracking.openAppSettings
import com.motolink.logic.OemGuide
import com.motolink.ui.InfoRow
import com.motolink.ui.PageHeader
import com.motolink.ui.Palette
import com.motolink.ui.SectionLabel
import com.motolink.ui.SwitchRow
import com.motolink.ui.container
import kotlinx.coroutines.launch

/**
 * "Cài đặt và quyền": rows with switches like the system settings. Permissions cannot be flipped from inside the app,
 * so those rows show the real state and open the system page when tapped.
 */
@Composable
fun SettingsScreen(onBack: () -> Unit, onLogin: () -> Unit) {
    val c = container()
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val signedIn by c.auth.signedIn.collectAsStateWithLifecycle(initialValue = false)
    val perms = LocationPermissions.read(ctx)
    val guide = OemGuide.forManufacturer(Build.MANUFACTURER)
    val openSettings = { openAppSettings(ctx) }

    Column(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        PageHeader("Cài đặt và quyền", onBack)
        Column(Modifier.verticalScroll(rememberScrollState()).navigationBarsPadding()) {
            SectionLabel("Chia sẻ vị trí")
            SwitchRow(
                "Vị trí chính xác",
                if (perms.approximateOnly) "Bạn mới cấp vị trí xấp xỉ, hành trình sẽ kém chính xác" else "Cho phép khi đi cùng nhóm",
                perms.fine, { openSettings() },
            )
            SwitchRow("Chạy trong nền", "Giữ vị trí khi tắt màn hình", perms.background, { openSettings() })
            SwitchRow("Thông báo", "Cảnh báo SOS và trạng thái ghi hành trình", perms.notifications, { openSettings() })

            SectionLabel("Quyền riêng tư")
            InfoRow(
                "Rút lại đồng ý dùng vị trí", "Dừng ghi ngay và ngừng chia sẻ với mọi chuyến đi",
                onClick = {
                    // Withdrawing consent stops recording immediately (docs/design/11 §5.1).
                    TrackingService.end(ctx)
                    scope.launch {
                        c.prefs.withdrawLocationConsent()
                        c.trips.withdrawLocationConsent()
                    }
                },
            ) { Icon(Icons.Filled.KeyboardArrowRight, null, Modifier.padding(start = 8.dp)) }

            SectionLabel("Giữ ứng dụng chạy khi tắt màn hình")
            Text(guide.brand, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp))
            guide.steps.forEach { Text("• $it", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 20.dp, vertical = 3.dp)) }
            Text(
                "Các bước có thể khác tùy phiên bản hệ điều hành. Việc này chỉ giảm khả năng bị tắt ngầm, không đảm bảo tuyệt đối.",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
            )

            SectionLabel("Tài khoản")
            HorizontalDivider(color = MaterialTheme.colorScheme.outline)
            if (signedIn) {
                InfoRow("Đăng xuất", "Xóa dữ liệu chuyến đi khỏi máy này", onClick = { scope.launch { c.signOut() } }) {
                    Text("Đăng xuất", color = Palette.Red, style = MaterialTheme.typography.labelMedium)
                }
            } else {
                InfoRow("Tài khoản", "Đăng nhập để đồng bộ chuyến đi", onClick = onLogin) { Icon(Icons.Filled.KeyboardArrowRight, null) }
            }
            HorizontalDivider(color = MaterialTheme.colorScheme.outline)
            Text("MotoLink ${com.motolink.BuildConfig.VERSION_NAME}", style = MaterialTheme.typography.bodySmall, color = Color.Gray, modifier = Modifier.padding(20.dp))
        }
    }
}
