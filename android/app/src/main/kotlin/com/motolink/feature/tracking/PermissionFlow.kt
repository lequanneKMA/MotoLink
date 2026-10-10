package com.motolink.feature.tracking

import android.Manifest
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.motolink.R
import com.motolink.core.location.LocationPermissions
import com.motolink.core.net.LocalPrefs
import com.motolink.ui.container
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/**
 * Permission order (docs/design/12 §5.1, ADR-013, D-04):
 *  1. prominent disclosure + local consent  (also Play's "prominent disclosure" [CHƯA XÁC MINH: U1])
 *  2. fine/coarse location (while in use)
 *  3. notifications (Android 13+)           -> recording can start here
 *  4. background location, via Settings on Android 11+ (never blocks recording)
 */
@Stable
class PermissionFlow internal constructor(
    private val ctx: Context,
    private val requestForeground: () -> Unit,
    private val requestNotifications: () -> Unit,
    private val requestBackgroundDirect: () -> Unit,
    private val setStep: (Step) -> Unit,
    private val getStep: () -> Step,
    private val onReadyCb: () -> Unit,
    private val accept: suspend () -> Unit,
) {
    enum class Step { IDLE, DISCLOSURE, FOREGROUND, NOTIFICATIONS, BACKGROUND_RATIONALE, DENIED }

    val step: Step get() = getStep()

    /** Entry point of the Start button. */
    suspend fun begin(consented: Boolean) {
        setStep(if (consented) Step.FOREGROUND else Step.DISCLOSURE)
        if (consented) advance()
    }

    suspend fun onDisclosureAccepted() {
        accept()
        setStep(Step.FOREGROUND)
        advance()
    }

    fun onDisclosureDeclined() = setStep(Step.IDLE)

    fun advance() {
        val p = LocationPermissions.read(ctx)
        when {
            !p.canRecord -> { setStep(Step.FOREGROUND); requestForeground() }
            !p.notifications -> { setStep(Step.NOTIFICATIONS); requestNotifications() }
            else -> {
                setStep(Step.IDLE)
                onReadyCb() // recording may start now
                if (!p.background && Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) setStep(Step.BACKGROUND_RATIONALE)
            }
        }
    }

    fun onForegroundResult() {
        val p = LocationPermissions.read(ctx)
        if (!p.canRecord) setStep(Step.DENIED) else advance()
    }

    fun onNotificationsResult() = advance() // recording still works if denied; the notification is just hidden

    fun openBackgroundSettings() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) openAppSettings(ctx) else requestBackgroundDirect()
        setStep(Step.IDLE)
    }

    fun skipBackground() = setStep(Step.IDLE)
    fun dismissDenied() = setStep(Step.IDLE)
}

fun openAppSettings(ctx: Context) {
    ctx.startActivity(
        Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", ctx.packageName, null)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
    )
}

@Composable
fun rememberPermissionFlow(onReady: () -> Unit): PermissionFlow {
    val ctx = LocalContext.current
    val prefs = container().prefs
    var step by remember { mutableStateOf(PermissionFlow.Step.IDLE) }
    lateinit var flow: PermissionFlow

    val fg = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { flow.onForegroundResult() }
    val notif = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { flow.onNotificationsResult() }
    val bg = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { /* best effort */ }

    flow = remember(ctx) {
        PermissionFlow(
            ctx = ctx,
            requestForeground = { fg.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)) },
            requestNotifications = {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) notif.launch(Manifest.permission.POST_NOTIFICATIONS) else flow.advance()
            },
            requestBackgroundDirect = { bg.launch(Manifest.permission.ACCESS_BACKGROUND_LOCATION) },
            setStep = { step = it },
            getStep = { step },
            onReadyCb = onReady,
            accept = { prefs.acceptLocationConsent(LocalPrefs.LOCATION_CONSENT_VERSION) },
        )
    }
    PermissionDialogs(flow)
    return flow
}

@Composable
private fun PermissionDialogs(flow: PermissionFlow) {
    val scope = rememberCoroutineScope()
    when (flow.step) {
        PermissionFlow.Step.DISCLOSURE -> AlertDialog(
            onDismissRequest = flow::onDisclosureDeclined,
            title = { Text(stringResource(R.string.perm_disclosure_title)) },
            text = { Column(Modifier.verticalScroll(rememberScrollState())) { Text(stringResource(R.string.perm_disclosure_body)) } },
            confirmButton = { TextButton(onClick = { scope.launch { flow.onDisclosureAccepted() } }) { Text(stringResource(R.string.perm_agree)) } },
            dismissButton = { TextButton(onClick = flow::onDisclosureDeclined) { Text(stringResource(R.string.perm_decline)) } },
        )
        PermissionFlow.Step.BACKGROUND_RATIONALE -> {
            val ctx = LocalContext.current
            val label = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) ctx.packageManager.backgroundPermissionOptionLabel.toString() else ""
            AlertDialog(
                onDismissRequest = flow::skipBackground,
                title = { Text(stringResource(R.string.perm_background_title)) },
                text = { Text(stringResource(R.string.perm_background_body, label), Modifier.padding(top = 4.dp)) },
                confirmButton = { TextButton(onClick = flow::openBackgroundSettings) { Text(stringResource(R.string.perm_open_settings)) } },
                dismissButton = { TextButton(onClick = flow::skipBackground) { Text(stringResource(R.string.perm_skip)) } },
            )
        }
        PermissionFlow.Step.DENIED -> AlertDialog(
            onDismissRequest = flow::dismissDenied,
            text = { Text(stringResource(R.string.perm_denied_location)) },
            confirmButton = {
                val ctx = LocalContext.current
                TextButton(onClick = { openAppSettings(ctx); flow.dismissDenied() }) { Text(stringResource(R.string.perm_open_settings)) }
            },
            dismissButton = { TextButton(onClick = flow::dismissDenied) { Text(stringResource(R.string.perm_skip)) } },
        )
        else -> Unit
    }
}

/** Reads whether the user already accepted the current disclosure version. */
suspend fun LocalPrefs.hasLocationConsent(): Boolean = locationConsentVersion.first() >= LocalPrefs.LOCATION_CONSENT_VERSION
