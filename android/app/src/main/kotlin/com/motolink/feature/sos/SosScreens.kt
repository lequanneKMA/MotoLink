package com.motolink.feature.sos

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.motolink.feature.map.rememberMapStyleUrl
import com.motolink.feature.map.LiveMap
import com.motolink.feature.map.MapMarker
import com.motolink.logic.SosAlert
import com.motolink.ui.Palette
import com.motolink.ui.PageHeader
import com.motolink.ui.PrimaryButton
import org.maplibre.android.geometry.LatLng

/** Kinds offered when raising an SOS, in the order of the design: one tap sends (no form). */
data class SosReason(val kind: String, val label: String, val symbol: String, val urgent: Boolean)

val SosReasons = listOf(
    SosReason("breakdown", "Hỏng xe", "XE", false),
    SosReason("accident", "Tai nạn", "!", true),
    SosReason("lost", "Bị lạc", "?", false),
    SosReason("medical", "Cần y tế", "+", true),
    SosReason("general", "Khác", "…", false),
)

fun reasonLabel(kind: String) = SosReasons.firstOrNull { it.kind == kind }?.label ?: "Cần giúp đỡ"

/** "Bạn cần giúp gì?" — a list of big buttons; a single tap sends to the whole group. */
@Composable
fun SendSosPage(onPick: (SosReason) -> Unit, onCancel: () -> Unit) {
    Column(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        PageHeader("Bạn cần giúp gì?", onBack = onCancel)
        Text(
            "Chạm một lần để gửi ngay cho cả nhóm", style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 20.dp, vertical = 6.dp),
        )
        Column(Modifier.weight(1f).padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            SosReasons.forEach { r ->
                val border = if (r.urgent) Palette.Red else MaterialTheme.colorScheme.outline
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 84.dp).clip(RoundedCornerShape(18.dp))
                        .border(2.dp, border, RoundedCornerShape(18.dp)).clickable { onPick(r) }.padding(horizontal = 16.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    Box(
                        Modifier.size(52.dp).background(if (r.urgent) Palette.Red else MaterialTheme.colorScheme.primary, CircleShape),
                        contentAlignment = Alignment.Center,
                    ) { Text(r.symbol, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Black) }
                    Text(r.label, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(30.dp))
                }
            }
        }
        OutlinedButton(
            onClick = onCancel, shape = RoundedCornerShape(14.dp),
            modifier = Modifier.fillMaxWidth().padding(16.dp).navigationBarsPadding().heightIn(min = 60.dp),
        ) { Text("Hủy", style = MaterialTheme.typography.labelLarge) }
    }
}

enum class SentState { SENDING, OFFLINE, DELIVERED }

/** Result of the SOS: sending, waiting for signal, or delivered to N people. */
@Composable
fun SosSentPage(state: SentState, recipients: Int, onDone: () -> Unit) {
    val (title, sub) = when (state) {
        SentState.SENDING -> "Đang gửi…" to "Giữ nguyên, đang gửi vị trí của bạn"
        SentState.OFFLINE -> "Chưa có mạng" to "Sẽ tự gửi khi có sóng"
        SentState.DELIVERED -> "Đã gửi cho $recipients người" to "Cả nhóm đã nhận được vị trí của bạn"
    }
    Column(
        Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).statusBarsPadding().navigationBarsPadding().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Box(
                Modifier.size(112.dp).background(if (state == SentState.DELIVERED) MaterialTheme.colorScheme.primary else Palette.Amber, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                Icon(if (state == SentState.DELIVERED) Icons.Filled.Check else Icons.Filled.Schedule, null, Modifier.size(60.dp), tint = Color.White)
            }
            Spacer(Modifier.size(24.dp))
            Text(title, style = MaterialTheme.typography.headlineMedium, textAlign = TextAlign.Center)
            Spacer(Modifier.size(8.dp))
            Text(sub, style = MaterialTheme.typography.bodyLarge, textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        PrimaryButton("Về bản đồ", onDone)
    }
}

/**
 * Full-screen alert for an SOS from another member: red, one name, one reason, two big buttons. Nothing else.
 * "Chỉ đường tới" hands the position to the phone's navigation app (the app has no routing engine of its own).
 */
@Composable
fun ReceiveSosPage(
    sos: SosAlert, me: LatLng?, onKnown: () -> Unit,
) {
    val ctx = LocalContext.current
    val at = if (sos.lat != null && sos.lng != null) LatLng(sos.lat!!, sos.lng!!) else null
    val name = (sos.senderName ?: "Một thành viên").uppercase()
    Column(
        Modifier.fillMaxSize().background(Palette.Red).statusBarsPadding().navigationBarsPadding().padding(horizontal = 18.dp, vertical = 22.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Column(Modifier.weight(0.30f), verticalArrangement = Arrangement.Center) {
            Text("$name CẦN GIÚP", color = Color.White, fontSize = 40.sp, fontWeight = FontWeight.Black, lineHeight = 42.sp)
            Text(reasonLabel(sos.kind), color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.ExtraBold, modifier = Modifier.padding(top = 10.dp))
            if (!sos.message.isNullOrBlank()) Text(sos.message!!, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = 6.dp))
        }
        Box(Modifier.weight(0.36f).fillMaxWidth().clip(RoundedCornerShape(18.dp)).border(5.dp, Color.White, RoundedCornerShape(18.dp))) {
            if (at != null) {
                val markers = listOfNotNull(
                    MapMarker("sos", at, (sos.senderName ?: "?").take(1).uppercase(), Palette.Red, label = sos.senderName, isSos = true),
                    me?.let { MapMarker("me", it, "B", Color(0xFF1565C0), selfArrow = true) },
                )
                LiveMap(markers = markers, fitSignal = 0, dark = false, styleUrl = rememberMapStyleUrl())
            } else {
                Box(Modifier.fillMaxSize().background(Color(0xFFB71C1C)), contentAlignment = Alignment.Center) {
                    Text("Không có vị trí kèm theo", color = Color.White, style = MaterialTheme.typography.bodyLarge)
                }
            }
        }
        Column(Modifier.weight(0.34f), verticalArrangement = Arrangement.spacedBy(12.dp, Alignment.Bottom)) {
            if (at != null) {
                Button(
                    onClick = { openNavigation(ctx, at, sos.senderName ?: "SOS") }, shape = RoundedCornerShape(14.dp),
                    colors = ButtonDefaults.buttonColors(containerColor = Color.White, contentColor = Palette.RedDark),
                    modifier = Modifier.fillMaxWidth().heightIn(min = 68.dp),
                ) { Text("Chỉ đường tới", style = MaterialTheme.typography.titleMedium) }
            }
            OutlinedButton(
                onClick = onKnown, shape = RoundedCornerShape(14.dp), border = BorderStroke(3.dp, Color.White),
                colors = ButtonDefaults.outlinedButtonColors(contentColor = Color.White),
                modifier = Modifier.fillMaxWidth().heightIn(min = 68.dp),
            ) { Text("Đã biết", style = MaterialTheme.typography.titleMedium) }
        }
    }
}

private fun openNavigation(ctx: Context, at: LatLng, label: String) {
    val uri = Uri.parse("geo:${at.latitude},${at.longitude}?q=${at.latitude},${at.longitude}(${Uri.encode(label)})")
    try {
        ctx.startActivity(Intent(Intent.ACTION_VIEW, uri).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        // no navigation app installed: the position is still on the map above
    }
}
