package com.motolink.feature.auth

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import com.motolink.ui.LabeledField
import com.motolink.ui.PageHeader
import com.motolink.ui.PrimaryButton
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.motolink.R
import com.motolink.core.net.ApiFailure
import com.motolink.ui.containerViewModel

@Composable
fun errorText(f: ApiFailure): String = when (f) {
    ApiFailure.Network -> stringResource(R.string.error_network)
    is ApiFailure.Server -> when (f.code) {
        "invalid_credentials" -> stringResource(R.string.error_invalid_credentials)
        "email_taken" -> stringResource(R.string.error_email_taken)
        "rate_limited" -> stringResource(R.string.error_rate_limited)
        "validation_failed" -> stringResource(R.string.error_validation) +
            (f.fields.takeIf { it.isNotEmpty() }?.joinToString(prefix = " (", postfix = ")") { it.field } ?: "")
        else -> stringResource(R.string.error_generic)
    }
    is ApiFailure.Unknown -> stringResource(R.string.error_generic)
}

@Composable
fun LoginScreen(onDone: () -> Unit, onGoRegister: () -> Unit, onSkip: () -> Unit) {
    val vm = containerViewModel { AuthViewModel(it.auth) }
    val st by vm.state.collectAsStateWithLifecycle()
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(st.done) { if (st.done) onDone() }

    AuthScaffold(stringResource(R.string.login_title), st, onBack = null) {
        EmailField(email) { email = it }
        PasswordField(password) { password = it }
        PrimaryButton(stringResource(R.string.action_login), { vm.login(email, password) }, enabled = !st.loading && email.isNotBlank() && password.isNotEmpty())
        TextButton(onClick = onGoRegister, modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp)) { Text(stringResource(R.string.go_register), style = MaterialTheme.typography.bodyLarge) }
        TextButton(onClick = onSkip, modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp)) { Text("Bỏ qua, dùng ngoại tuyến", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

@Composable
fun RegisterScreen(onDone: () -> Unit, onGoLogin: () -> Unit) {
    val vm = containerViewModel { AuthViewModel(it.auth) }
    val st by vm.state.collectAsStateWithLifecycle()
    var name by rememberSaveable { mutableStateOf("") }
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var terms by rememberSaveable { mutableStateOf(false) }
    var privacy by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(st.done) { if (st.done) onDone() }

    AuthScaffold(stringResource(R.string.register_title), st, onBack = onGoLogin) {
        LabeledField(stringResource(R.string.display_name), name, { name = it.take(50) }, placeholder = "Tên mọi người sẽ thấy")
        EmailField(email) { email = it }
        PasswordField(password) { password = it }
        // Two separate, unticked boxes: consent must be explicit (docs/design/11 §5.1).
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = terms, onCheckedChange = { terms = it })
            Text(stringResource(R.string.accept_terms), style = MaterialTheme.typography.bodyMedium)
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = privacy, onCheckedChange = { privacy = it })
            Text(stringResource(R.string.accept_privacy), style = MaterialTheme.typography.bodyMedium)
        }
        PrimaryButton(
            stringResource(R.string.action_register), { vm.register(email, password, name) },
            enabled = !st.loading && terms && privacy && name.isNotBlank() && email.isNotBlank() && password.length >= 8,
        )
        TextButton(onClick = onGoLogin, modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp)) { Text(stringResource(R.string.go_login), style = MaterialTheme.typography.bodyLarge) }
    }
}

@Composable
private fun AuthScaffold(title: String, st: AuthUiState, onBack: (() -> Unit)?, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).navigationBarsPadding().imePadding()) {
        PageHeader(title, onBack)
        Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Column(Modifier.fillMaxWidth().padding(vertical = 12.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.size(72.dp).background(MaterialTheme.colorScheme.primary, RoundedCornerShape(22.dp)), contentAlignment = Alignment.Center) {
                    Text("M", color = Color.White, fontSize = 40.sp, fontWeight = FontWeight.Black)
                }
                Text("MotoLink", style = MaterialTheme.typography.headlineMedium)
                Text("Giữ liên lạc trên mọi cung đường", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            content()
            if (st.loading) CircularProgressIndicator(Modifier.align(Alignment.CenterHorizontally))
            st.failure?.let { Text(errorText(it), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyLarge) }
        }
    }
}

@Composable
private fun EmailField(value: String, onChange: (String) -> Unit) =
    LabeledField(stringResource(R.string.email), value, onChange, placeholder = "ban@email.com", keyboard = KeyboardType.Email)

@Composable
private fun PasswordField(value: String, onChange: (String) -> Unit) =
    LabeledField(stringResource(R.string.password), value, onChange, placeholder = "Ít nhất 8 ký tự", password = true)
