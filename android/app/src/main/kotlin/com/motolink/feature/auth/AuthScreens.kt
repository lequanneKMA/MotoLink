package com.motolink.feature.auth

import androidx.compose.foundation.layout.Arrangement
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

    AuthScaffold(stringResource(R.string.login_title), st) {
        EmailField(email) { email = it }
        PasswordField(password) { password = it }
        Button(
            onClick = { vm.login(email, password) },
            enabled = !st.loading && email.isNotBlank() && password.isNotEmpty(),
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(R.string.action_login)) }
        TextButton(onClick = onGoRegister) { Text(stringResource(R.string.go_register)) }
        TextButton(onClick = onSkip) { Text("Bỏ qua, dùng ngoại tuyến") }
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

    AuthScaffold(stringResource(R.string.register_title), st) {
        OutlinedTextField(
            value = name, onValueChange = { name = it.take(50) }, singleLine = true,
            label = { Text(stringResource(R.string.display_name)) }, modifier = Modifier.fillMaxWidth(),
        )
        EmailField(email) { email = it }
        PasswordField(password) { password = it }
        // Two separate, unticked boxes: consent must be explicit (docs/design/11 §5.1).
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = terms, onCheckedChange = { terms = it })
            Text(stringResource(R.string.accept_terms))
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = privacy, onCheckedChange = { privacy = it })
            Text(stringResource(R.string.accept_privacy))
        }
        Button(
            onClick = { vm.register(email, password, name) },
            enabled = !st.loading && terms && privacy && name.isNotBlank() && email.isNotBlank() && password.length >= 8,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(R.string.action_register)) }
        TextButton(onClick = onGoLogin) { Text(stringResource(R.string.go_login)) }
    }
}

@Composable
private fun AuthScaffold(title: String, st: AuthUiState, content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Spacer(Modifier.height(32.dp))
        Text(title, style = MaterialTheme.typography.headlineMedium)
        content()
        if (st.loading) CircularProgressIndicator()
        st.failure?.let { Text(errorText(it), color = MaterialTheme.colorScheme.error) }
    }
}

@Composable
private fun EmailField(value: String, onChange: (String) -> Unit) = OutlinedTextField(
    value = value, onValueChange = onChange, singleLine = true,
    label = { Text(stringResource(R.string.email)) },
    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
    modifier = Modifier.fillMaxWidth(),
)

@Composable
private fun PasswordField(value: String, onChange: (String) -> Unit) = OutlinedTextField(
    value = value, onValueChange = onChange, singleLine = true,
    label = { Text(stringResource(R.string.password)) },
    visualTransformation = PasswordVisualTransformation(),
    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
    modifier = Modifier.fillMaxWidth(),
)
