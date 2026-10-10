package com.motolink

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.motolink.feature.auth.LoginScreen
import com.motolink.feature.auth.RegisterScreen
import com.motolink.feature.home.HistoryDetailScreen
import com.motolink.feature.home.HistoryScreen
import com.motolink.feature.home.HomeScreen
import com.motolink.feature.settings.SettingsScreen
import com.motolink.ui.MotoLinkTheme
import com.motolink.ui.container
import kotlinx.coroutines.flow.first

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { MotoLinkTheme { AppNav() } }
    }
}

private object Routes {
    const val LOGIN = "login"
    const val REGISTER = "register"
    const val HOME = "home"
    const val HISTORY = "history"
    const val HISTORY_DETAIL = "history/{id}"
    const val SETTINGS = "settings"
    fun detail(id: String) = "history/" + java.net.URLEncoder.encode(id, "UTF-8")
}

@Composable
private fun AppNav() {
    val nav = rememberNavController()
    val c = container()

    // Signed-in users go straight to Home; others see Login but can skip (solo recording works offline).
    var start by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { start = if (c.auth.signedIn.first()) Routes.HOME else Routes.LOGIN }
    // The server rejected our refresh token: back to login, without losing local recordings.
    LaunchedEffect(Unit) {
        c.sessionEvents.expired.collect { nav.navigate(Routes.LOGIN) { popUpTo(0) } }
    }
    val startDest = start ?: return

    NavHost(navController = nav, startDestination = startDest) {
        composable(Routes.LOGIN) {
            LoginScreen(
                onDone = { nav.navigate(Routes.HOME) { popUpTo(0) } },
                onGoRegister = { nav.navigate(Routes.REGISTER) },
                onSkip = { nav.navigate(Routes.HOME) { popUpTo(0) } },
            )
        }
        composable(Routes.REGISTER) {
            RegisterScreen(
                onDone = { nav.navigate(Routes.HOME) { popUpTo(0) } },
                onGoLogin = { nav.popBackStack() },
            )
        }
        composable(Routes.HOME) {
            HomeScreen(onHistory = { nav.navigate(Routes.HISTORY) }, onSettings = { nav.navigate(Routes.SETTINGS) })
        }
        composable(Routes.HISTORY) {
            HistoryScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Routes.detail(it)) })
        }
        composable(Routes.HISTORY_DETAIL) { entry ->
            val id = java.net.URLDecoder.decode(entry.arguments?.getString("id").orEmpty(), "UTF-8")
            HistoryDetailScreen(sessionId = id, onBack = { nav.popBackStack() })
        }
        composable(Routes.SETTINGS) {
            SettingsScreen(onBack = { nav.popBackStack() }, onLogin = { nav.navigate(Routes.LOGIN) })
        }
    }
}
