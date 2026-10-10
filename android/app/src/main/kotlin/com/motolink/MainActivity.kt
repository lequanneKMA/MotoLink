package com.motolink

import android.content.Intent
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.navArgument
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.motolink.feature.auth.LoginScreen
import com.motolink.feature.notifications.AppNotifications
import com.motolink.feature.notifications.NotificationsScreen
import com.motolink.feature.trips.TripDetailScreen
import com.motolink.feature.trips.TripsScreen
import com.motolink.feature.auth.RegisterScreen
import com.motolink.feature.home.HistoryDetailScreen
import com.motolink.feature.home.HistoryScreen
import com.motolink.feature.home.HomeScreen
import com.motolink.feature.settings.SettingsScreen
import com.motolink.ui.MotoLinkTheme
import com.motolink.ui.container
import kotlinx.coroutines.flow.first

/** A trip to open because of a notification tap (push or local SOS alert). */
data class DeepLink(val tripId: String, val sos: Boolean)

class MainActivity : ComponentActivity() {
    private val deepLink = mutableStateOf<DeepLink?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        readLink(intent)
        setContent { MotoLinkTheme { AppNav(deepLink.value) { deepLink.value = null } } }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        readLink(intent)
    }

    private fun readLink(i: Intent?) {
        val trip = i?.getStringExtra(AppNotifications.EXTRA_TRIP_ID)?.takeIf { it.isNotBlank() } ?: return
        deepLink.value = DeepLink(trip, i.getBooleanExtra(AppNotifications.EXTRA_OPEN_SOS, false) || i.getStringExtra("type") == "sos")
    }
}

private object Routes {
    const val LOGIN = "login"
    const val REGISTER = "register"
    const val HOME = "home"
    const val HISTORY = "history"
    const val HISTORY_DETAIL = "history/{id}"
    const val SETTINGS = "settings"
    const val TRIPS = "trips"
    const val TRIP = "trip/{id}?sos={sos}"
    const val NOTIFICATIONS = "notifications"
    fun trip(id: String, sos: Boolean = false) = "trip/" + java.net.URLEncoder.encode(id, "UTF-8") + "?sos=" + sos
    fun detail(id: String) = "history/" + java.net.URLEncoder.encode(id, "UTF-8")
}

@Composable
private fun AppNav(link: DeepLink?, onLinkHandled: () -> Unit) {
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
    val signedIn by c.auth.signedIn.collectAsStateWithLifecycle(initialValue = false)
    LaunchedEffect(link, start) {
        if (link != null && start != null) {
            nav.navigate(Routes.trip(link.tripId, link.sos))
            onLinkHandled()
        }
    }

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
            HomeScreen(
                onHistory = { nav.navigate(Routes.HISTORY) },
                onSettings = { nav.navigate(Routes.SETTINGS) },
                onTrips = { nav.navigate(Routes.TRIPS) },
                onNotifications = { nav.navigate(Routes.NOTIFICATIONS) },
            )
        }
        composable(Routes.TRIPS) {
            TripsScreen(
                onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Routes.trip(it)) },
                signedIn = signedIn, onLogin = { nav.navigate(Routes.LOGIN) },
            )
        }
        composable(
            Routes.TRIP,
            arguments = listOf(
                navArgument("id") { type = NavType.StringType },
                navArgument("sos") { type = NavType.BoolType; defaultValue = false },
            ),
        ) { entry ->
            val id = java.net.URLDecoder.decode(entry.arguments?.getString("id").orEmpty(), "UTF-8")
            TripDetailScreen(tripId = id, onBack = { nav.popBackStack() })
        }
        composable(Routes.NOTIFICATIONS) {
            NotificationsScreen(onBack = { nav.popBackStack() }, onOpenTrip = { nav.navigate(Routes.trip(it)) })
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
