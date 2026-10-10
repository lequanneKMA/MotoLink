package com.motolink.feature.auth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.motolink.core.net.ApiFailure
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AuthUiState(
    val loading: Boolean = false,
    val failure: ApiFailure? = null,
    val done: Boolean = false,
)

class AuthViewModel(private val repo: AuthRepository) : ViewModel() {
    private val _state = MutableStateFlow(AuthUiState())
    val state: StateFlow<AuthUiState> = _state.asStateFlow()

    fun login(email: String, password: String) = run { repo.login(email, password) }

    fun register(email: String, password: String, name: String) = run { repo.register(email, password, name) }

    fun clearError() = _state.update { it.copy(failure = null) }

    private fun run(block: suspend () -> AuthResult) {
        if (_state.value.loading) return
        _state.update { AuthUiState(loading = true) }
        viewModelScope.launch {
            when (val r = block()) {
                is AuthResult.Ok -> _state.update { AuthUiState(done = true) }
                is AuthResult.Failed -> _state.update { AuthUiState(failure = r.failure) }
            }
        }
    }
}
