package com.fitsync.nativeapp

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

private val Ink = Color(0xFF20332F)
private val Teal = Color(0xFF176B59)
private val Palette = lightColorScheme(
    primary = Teal, onPrimary = Color.White,
    primaryContainer = Color(0xFFE0F1EA), onPrimaryContainer = Ink,
    secondary = Color(0xFF536A63), onSecondary = Color.White,
    secondaryContainer = Color(0xFFE8EFEB), onSecondaryContainer = Ink,
    tertiary = Teal, onTertiary = Color.White,
    background = Color(0xFFF5F7F6), onBackground = Ink,
    surface = Color.White, onSurface = Ink,
    surfaceVariant = Color(0xFFEDF2EF), onSurfaceVariant = Color(0xFF61716A),
    surfaceContainer = Color(0xFFF0F4F1), surfaceContainerHigh = Color(0xFFE8EFEB),
    outline = Color(0xFF889A91), outlineVariant = Color(0xFFDDE5DF),
    error = Color(0xFFAD3E3E), onError = Color.White,
    errorContainer = Color(0xFFFFEEEB), onErrorContainer = Color(0xFF74302D),
)

@Composable
internal fun FitSyncTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = Palette,
        typography = Typography(
            headlineLarge = TextStyle(fontSize = 30.sp, lineHeight = 36.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.6).sp),
            headlineMedium = TextStyle(fontSize = 25.sp, lineHeight = 32.sp, fontWeight = FontWeight.Bold),
            titleLarge = TextStyle(fontSize = 21.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold),
            titleMedium = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold),
            bodyLarge = TextStyle(fontSize = 16.sp, lineHeight = 25.sp),
            bodyMedium = TextStyle(fontSize = 14.sp, lineHeight = 22.sp),
            labelLarge = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold),
        ),
        shapes = Shapes(small = RoundedCornerShape(10.dp), medium = RoundedCornerShape(16.dp), large = RoundedCornerShape(20.dp)),
        content = content,
    )
}

/** Matching line icons without a font glyph or an additional icon dependency. */
@Composable
internal fun TabIcon(tab: String) {
    val color = LocalContentColor.current
    Canvas(Modifier.size(24.dp)) {
        val unit = size.width / 24f
        fun point(x: Float, y: Float) = Offset(x * unit, y * unit)
        fun line(x: Float, y: Float, endX: Float, endY: Float) = drawLine(color, point(x, y), point(endX, endY), 1.8f * unit)
        val stroke = Stroke(1.8f * unit)
        when (tab) {
            "today" -> {
                drawRoundRect(color, point(4f, 5f), Size(16f * unit, 16f * unit), CornerRadius(3f * unit), style = stroke)
                line(4f, 10f, 20f, 10f); line(8f, 3f, 8f, 7f); line(16f, 3f, 16f, 7f)
                drawCircle(color, 2f * unit, point(12f, 15f))
            }
            "plan" -> {
                drawRoundRect(color, point(5f, 3f), Size(14f * unit, 18f * unit), CornerRadius(2f * unit), style = stroke)
                for (y in listOf(8f, 12f, 16f)) line(9f, y, 15f, y)
            }
            "history" -> {
                drawCircle(color, 8f * unit, point(12f, 12f), style = stroke)
                line(12f, 7f, 12f, 12f); line(12f, 12f, 16f, 14f)
            }
            else -> {
                drawCircle(color, 3.5f * unit, point(12f, 7f), style = stroke)
                drawRoundRect(color, point(5f, 14f), Size(14f * unit, 7f * unit), CornerRadius(3f * unit), style = stroke)
            }
        }
    }
}
