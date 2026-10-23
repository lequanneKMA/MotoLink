package com.motolink.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/**
 * Design tokens from the Figma Make prototype (docs/design-ui): readable at a glance on a motorbike, so only two
 * working text sizes (14 and 20 sp), heavy weights, high contrast, red reserved for SOS and danger.
 * No dynamic (wallpaper) colour: the brand colours carry meaning.
 */
object Palette {
    val Green = Color(0xFF0A5F26)
    val GreenDark = Color(0xFF064A1D)
    val GreenSoft = Color(0xFFD9EEE3)
    val Red = Color(0xFFD50000)
    val RedDark = Color(0xFF9B0000)
    val Ink = Color(0xFF111111)
    val Muted = Color(0xFF52615A)
    val Line = Color(0xFFD7DED9)
    val Surface = Color(0xFFFFFFFF)
    val SurfaceSoft = Color(0xFFF3F6F4)
    val Amber = Color(0xFFFFB300)
    val MapLight = Color(0xFFE9EEE6)
    val MapDark = Color(0xFF1B2420)

    // Dark counterparts
    val DarkSurface = Color(0xFF121A16)
    val DarkSurfaceSoft = Color(0xFF1C2621)
    val DarkInk = Color(0xFFF1F5F2)
    val DarkMuted = Color(0xFFA9B8AF)
    val DarkLine = Color(0xFF34423A)
    val DarkGreen = Color(0xFF52B96F)

    /** Member colours, in join order (the server assigns the same palette; docs/design/05). */
    val Members = listOf("#1565C0", "#E65100", "#6A1B9A", "#00838F", "#C2185B", "#455A64")
}

private val LightScheme = lightColorScheme(
    primary = Palette.Green, onPrimary = Color.White, primaryContainer = Palette.GreenSoft, onPrimaryContainer = Palette.GreenDark,
    secondary = Palette.GreenDark, error = Palette.Red, onError = Color.White,
    background = Palette.Surface, onBackground = Palette.Ink, surface = Palette.Surface, onSurface = Palette.Ink,
    surfaceVariant = Palette.SurfaceSoft, onSurfaceVariant = Palette.Muted, outline = Palette.Line,
)

private val DarkScheme = darkColorScheme(
    primary = Palette.DarkGreen, onPrimary = Color(0xFF00210B), primaryContainer = Color(0xFF0F3D20), onPrimaryContainer = Palette.GreenSoft,
    secondary = Palette.DarkGreen, error = Color(0xFFFF5252), onError = Color.Black,
    background = Palette.DarkSurface, onBackground = Palette.DarkInk, surface = Palette.DarkSurface, onSurface = Palette.DarkInk,
    surfaceVariant = Palette.DarkSurfaceSoft, onSurfaceVariant = Palette.DarkMuted, outline = Palette.DarkLine,
)

private val Sans = FontFamily.Default // Roboto on Android; Vietnamese diacritics render correctly

private fun t(size: Int, weight: FontWeight, line: Int = (size * 1.25).toInt()) =
    TextStyle(fontFamily = Sans, fontSize = size.sp, fontWeight = weight, lineHeight = line.sp)

private val AppTypography = Typography(
    headlineLarge = t(40, FontWeight.Black, 42),
    headlineMedium = t(28, FontWeight.ExtraBold, 32),
    titleLarge = t(24, FontWeight.ExtraBold),
    titleMedium = t(20, FontWeight.ExtraBold),
    titleSmall = t(14, FontWeight.Bold),
    bodyLarge = t(20, FontWeight.SemiBold),
    bodyMedium = t(14, FontWeight.Medium),
    bodySmall = t(14, FontWeight.Normal),
    labelLarge = t(20, FontWeight.ExtraBold),
    labelMedium = t(14, FontWeight.Bold),
    labelSmall = t(14, FontWeight.Medium),
)

@Composable
fun MotoLinkTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (isSystemInDarkTheme()) DarkScheme else LightScheme, typography = AppTypography, content = content)
}
