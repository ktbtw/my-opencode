package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import com.chatcodex.chat_codex_app.overlay.compose.FloatingMode

/** dp -> px。 */
internal fun Context.dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

/**
 * 悬浮层外壳：模式切换、几何位置持久化、状态同步。
 *
 * UI 本体在 Compose 实现里（GlobalOverlayCompose.kt + overlay/compose/），
 * 这里只保留服务与 Compose 之间的胶水逻辑。
 */

/** 重新渲染悬浮层（模式变化时重建窗口，其余情况只同步状态）。 */
internal fun GlobalOverlayService.renderOverlay() {
    if (stopping || destroyed || !overlayEnabled) return
    val state = ensureComposeState()
    state.mode = if (expanded) FloatingMode.WINDOW else FloatingMode.BALL
    renderComposeOverlay()
}

/** 内容变化：只同步状态，交给 Compose 重组，不重建窗口。 */
internal fun GlobalOverlayService.updatePanelContent() {
    if (stopping || destroyed || !overlayEnabled) return
    syncComposeState()
}

/** 悬浮球角标/配色变化：同样只同步状态。 */
internal fun GlobalOverlayService.updateBubble() {
    if (stopping || destroyed || !overlayEnabled) return
    syncComposeState()
}

/** 刷新悬浮球。 */
internal fun GlobalOverlayService.refreshBubble() {
    if (stopping || destroyed || !overlayEnabled) return
    syncComposeState()
}

/** 从偏好读取面板尺寸与位置。 */
internal fun GlobalOverlayService.restoreOverlayGeometry() {
    val prefs = getSharedPreferences(GEOMETRY_PREFS, Context.MODE_PRIVATE)
    val maxPanelWidth = (resources.displayMetrics.widthPixels - dp(24)).coerceAtLeast(dp(300))
    val maxPanelHeight = (resources.displayMetrics.heightPixels - dp(120)).coerceAtLeast(dp(380))
    panelWidthPx = dp(prefs.getInt("panel_width_dp", 360)).coerceIn(dp(300), maxPanelWidth)
    panelHeightPx = dp(prefs.getInt("panel_height_dp", 600)).coerceIn(dp(380), maxPanelHeight)
    panelX = prefs.getInt("panel_x", -1)
    panelY = prefs.getInt("panel_y", -1)
    bubbleX = prefs.getInt("bubble_x", -1)
    bubbleY = prefs.getInt("bubble_y", -1)
}

/** 把面板尺寸与位置写回偏好。 */
internal fun GlobalOverlayService.saveOverlayGeometry() {
    val density = resources.displayMetrics.density.coerceAtLeast(1f)
    getSharedPreferences(GEOMETRY_PREFS, Context.MODE_PRIVATE).edit()
        .putInt("panel_width_dp", (panelWidthPx / density + 0.5f).toInt())
        .putInt("panel_height_dp", (panelHeightPx / density + 0.5f).toInt())
        .putInt("panel_x", panelX)
        .putInt("panel_y", panelY)
        .putInt("bubble_x", bubbleX)
        .putInt("bubble_y", bubbleY)
        .apply()
}
