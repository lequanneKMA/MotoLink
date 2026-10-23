package com.motolink.feature.map

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.collectAsState
import com.motolink.BuildConfig
import com.motolink.core.net.ApiService
import com.motolink.core.net.apiCall
import com.motolink.core.net.ApiResult
import com.motolink.ui.container
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** What the server says about the map stack (GET /v1/config). Empty style = no self-hosted basemap yet (blank map). */
data class MapConfig(val styleLight: String? = null, val styleDark: String? = null, val routing: Boolean = false, val search: Boolean = false)

class MapConfigRepository(private val api: ApiService, private val base: String = BuildConfig.API_BASE_URL) {
    private val _state = MutableStateFlow(MapConfig())
    val state: StateFlow<MapConfig> = _state.asStateFlow()

    /** Called at start and after sign-in; failures keep the previous value (the app works without a basemap). */
    suspend fun refresh() {
        val r = apiCall { api.config() }
        if (r is ApiResult.Ok) {
            val t = r.value.tileStyleUrl
            fun url(theme: String) = if (t.isBlank()) null else (if (t.startsWith("http")) t else base.trimEnd('/') + t).replace("{theme}", theme)
            _state.value = MapConfig(url("light"), url("dark"), r.value.routingEnabled, r.value.searchEnabled)
        }
    }
}

/** Style URL for the current light/dark mode, or null for the blank fallback. */
@Composable
fun rememberMapStyleUrl(): String? {
    val cfg by container().mapConfig.state.collectAsState()
    return if (isSystemInDarkTheme()) cfg.styleDark else cfg.styleLight
}
