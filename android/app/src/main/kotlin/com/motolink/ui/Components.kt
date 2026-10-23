package com.motolink.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

fun parseHex(hex: String?, fallback: Color = Color(0xFF455A64)): Color =
    runCatching { Color(android.graphics.Color.parseColor(hex ?: return fallback)) }.getOrDefault(fallback)

/** Member colour for a stable index when the server did not send one. */
fun memberColor(index: Int): Color = parseHex(Palette.Members[index.mod(Palette.Members.size)])

@Composable
fun PrimaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, icon: ImageVector? = null) {
    Button(
        onClick = onClick, enabled = enabled, shape = RoundedCornerShape(14.dp),
        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.primary, contentColor = MaterialTheme.colorScheme.onPrimary),
        modifier = modifier.fillMaxWidth().heightIn(min = 60.dp),
    ) {
        if (icon != null) Icon(icon, null, Modifier.size(26.dp))
        Text(text, style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(start = if (icon != null) 10.dp else 0.dp))
    }
}

@Composable
fun SecondaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    OutlinedButton(
        onClick = onClick, enabled = enabled, shape = RoundedCornerShape(14.dp),
        border = BorderStroke(2.dp, MaterialTheme.colorScheme.primary),
        modifier = modifier.fillMaxWidth().heightIn(min = 60.dp),
    ) { Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary) }
}

@Composable
fun DangerButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    Button(
        onClick = onClick, enabled = enabled, shape = RoundedCornerShape(14.dp),
        colors = ButtonDefaults.buttonColors(containerColor = Palette.Red, contentColor = Color.White),
        modifier = modifier.fillMaxWidth().heightIn(min = 60.dp),
    ) { Text(text, style = MaterialTheme.typography.labelLarge) }
}

/** The round SOS button of the ride screen: 82 dp, white ring, impossible to miss and to miss-tap. */
@Composable
fun SosFab(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier
            .size(82.dp).shadow(6.dp, CircleShape).background(Palette.Red, CircleShape).border(5.dp, Color.White, CircleShape)
            .clickable(role = Role.Button, onClick = onClick).semantics { contentDescription = "Gửi SOS" },
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(58.dp).border(2.dp, Color.White, CircleShape), contentAlignment = Alignment.Center) {
            Text("SOS", color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Black)
        }
    }
}

/** Own speed, large and high contrast. */
@Composable
fun SpeedDial(kmh: Int?, modifier: Modifier = Modifier) {
    Column(
        modifier.size(80.dp).shadow(4.dp, CircleShape).background(Palette.Ink, CircleShape).border(3.dp, Color.White, CircleShape)
            .semantics { contentDescription = "${kmh ?: 0} ki-lô-mét một giờ" },
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
    ) {
        Text((kmh ?: 0).toString(), color = Color.White, fontSize = 30.sp, fontWeight = FontWeight.Black, lineHeight = 30.sp)
        Text("km/h", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Bold, lineHeight = 14.sp)
    }
}

/** Round white control floating over the map (menu, locate...). 56 dp so a gloved thumb hits it. */
@Composable
fun MapControl(icon: ImageVector, description: String, onClick: () -> Unit, modifier: Modifier = Modifier, tint: Color = Palette.Ink) {
    Surface(
        onClick = onClick, shape = CircleShape, color = Color.White, shadowElevation = 4.dp,
        border = BorderStroke(1.dp, Palette.Line), modifier = modifier.size(56.dp),
    ) {
        Box(Modifier.size(56.dp), contentAlignment = Alignment.Center) { Icon(icon, description, Modifier.size(30.dp), tint = tint) }
    }
}

@Composable
fun PageHeader(title: String, onBack: (() -> Unit)?, modifier: Modifier = Modifier, action: @Composable () -> Unit = {}) {
    Row(modifier.fillMaxWidth().statusBarsPadding().heightIn(min = 68.dp).padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        if (onBack != null) {
            IconButton(onClick = onBack, modifier = Modifier.size(56.dp)) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Quay lại", Modifier.size(30.dp)) }
        } else {
            Box(Modifier.size(16.dp))
        }
        Text(title, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f).padding(start = 4.dp))
        action()
    }
}

@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text.uppercase(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 22.dp, bottom = 6.dp), letterSpacing = 0.8.sp,
    )
}

@Composable
fun SwitchRow(label: String, description: String, checked: Boolean, onChange: (Boolean) -> Unit, enabled: Boolean = true) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 76.dp).clickable(enabled = enabled, role = Role.Switch) { onChange(!checked) }.padding(horizontal = 20.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyLarge)
            Text(description, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Switch(
            checked = checked, onCheckedChange = null, enabled = enabled,
            colors = SwitchDefaults.colors(checkedTrackColor = MaterialTheme.colorScheme.primary),
        )
    }
}

/** A plain row (label + description + trailing content). */
@Composable
fun InfoRow(label: String, description: String?, modifier: Modifier = Modifier, onClick: (() -> Unit)? = null, trailing: @Composable () -> Unit = {}) {
    Row(
        modifier.fillMaxWidth().heightIn(min = 76.dp).let { if (onClick != null) it.clickable(onClick = onClick) else it }.padding(horizontal = 20.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyLarge)
            if (description != null) Text(description, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        trailing()
    }
}

@Composable
fun LabeledField(
    label: String, value: String, onChange: (String) -> Unit, modifier: Modifier = Modifier,
    placeholder: String = "", keyboard: KeyboardType = KeyboardType.Text, password: Boolean = false,
    mono: Boolean = false,
) {
    Column(modifier.fillMaxWidth()) {
        Text(label, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(bottom = 6.dp))
        OutlinedTextField(
            value = value, onValueChange = onChange, singleLine = true,
            placeholder = { Text(placeholder, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant) },
            textStyle = MaterialTheme.typography.bodyLarge.copy(
                letterSpacing = if (mono) 3.sp else 0.sp,
                fontFamily = if (mono) androidx.compose.ui.text.font.FontFamily.Monospace else androidx.compose.ui.text.font.FontFamily.Default,
            ),
            shape = RoundedCornerShape(12.dp),
            keyboardOptions = KeyboardOptions(keyboardType = if (password) KeyboardType.Password else keyboard),
            visualTransformation = if (password) PasswordVisualTransformation() else VisualTransformation.None,
            modifier = Modifier.fillMaxWidth().heightIn(min = 62.dp),
        )
    }
}

/** Round avatar with the member's initial; used in lists and as the map marker. */
@Composable
fun MemberAvatar(initial: String, color: Color, size: Dp = 48.dp, alpha: Float = 1f, ring: Color = Color.White, modifier: Modifier = Modifier) {
    Box(
        modifier.size(size).alpha(alpha).background(color, CircleShape).border(3.dp, ring, CircleShape),
        contentAlignment = Alignment.Center,
    ) { Text(initial, color = Color.White, fontSize = (size.value * 0.46f).sp, fontWeight = FontWeight.Black) }
}

@Composable
fun CenteredHint(text: String, modifier: Modifier = Modifier) {
    Text(text, style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier.fillMaxWidth().padding(24.dp))
}
