package app.intercom.android.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

val Amber = Color(0xFFF5A524)
val ListenTeal = Color(0xFF2EC4B6)
val OnAirRed = Color(0xFFE5484D)
val VoxGreen = Color(0xFF46A758)
val Muted = Color(0xFF9AA3AD)

private val scheme = darkColorScheme(
    primary = Amber,
    onPrimary = Color.Black,
    secondary = ListenTeal,
    background = Color(0xFF0E1013),
    surface = Color(0xFF181B20),
    surfaceVariant = Color(0xFF22262D),
    surfaceContainerLow = Color(0xFF181B20),
    onBackground = Color(0xFFE8EAED),
    onSurface = Color(0xFFE8EAED),
    error = OnAirRed,
)

@Composable
fun IntercomTheme(content: @Composable () -> Unit) = MaterialTheme(colorScheme = scheme, content = content)

/** Channel color from the server ("#rrggbb"); falls back to amber on anything else. */
fun parseColor(hex: String): Color = runCatching { Color(android.graphics.Color.parseColor(hex)) }.getOrDefault(Amber)
