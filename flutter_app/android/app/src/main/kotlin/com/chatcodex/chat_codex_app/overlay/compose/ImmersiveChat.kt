package com.chatcodex.chat_codex_app.overlay.compose

import android.graphics.Bitmap
import android.os.Build
import android.view.WindowManager
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.PressInteraction
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Text
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.ImageShader
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.ShaderBrush
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.TileMode
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// —— 取自参考图的色值 ——
private val RefAiBubble = Color(0xFFE9ECFD)
private val RefUserBubble = Color(0xFFDCE6FF)
private val RefBubbleInk = Color(0xFF1B2740)
private val RefIcon = Color(0xFFF7FAFF)
private val RefMutedText = Color(0xFF7D8799)
private val RefSend = Color(0xFF8FB4F6)
// Operit 的 GlassyChip 用的是「黑 25% + 白 15% 描边」；
// 白底胶囊压在浅色玻璃上会糊成一片，深色玻璃反而更像玻璃。
private val RefChipBg = Color(0x40000000)
private val RefChipBorder = Color(0x26FFFFFF)

/**
 * 沉浸式全屏对话，按 Operit 截图复刻：
 * 纯白图标顶栏 / 左头像右气泡 / 底部能力胶囊 / 带光晕的白色输入条。
 */
@Composable
fun ImmersiveChat(
    state: OverlayUiState,
    onSend: (String) -> Unit,
    onCloseToBall: () -> Unit,
    onCollapseToWindow: () -> Unit,
    onAttach: () -> Unit,
    onVoiceStart: () -> Unit,
    onOcrStart: () -> Unit,
    onNewSession: () -> Unit,
    onSelectSession: (String) -> Unit,
    onSelectAgent: (String) -> Unit,
    onRemoveAttachment: (Int) -> Unit = {},
    onTypingChanged: (Boolean) -> Unit,
) {
    var showList by remember { mutableStateOf(false) }
    var typing by remember { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val keyboard = LocalSoftwareKeyboardController.current
    val screenHeight = LocalConfiguration.current.screenHeightDp.dp
    val keyboardSlot = screenHeight * 0.40f
    // 窗口现在盖住状态栏了（真沉浸），内容自己让出状态栏高度。
    val context = LocalContext.current
    val statusBarTop = remember(context) {
        val id = context.resources.getIdentifier("status_bar_height", "dimen", "android")
        if (id > 0) context.resources.getDimensionPixelSize(id) else 0
    }
    val statusBarDp = with(LocalDensity.current) { statusBarTop.toDp() }

    LaunchedEffect(state.messages.size) {
        if (state.messages.isNotEmpty()) {
            listState.animateScrollToItem(state.messages.lastIndex)
        }
    }

    // 点空白处清焦点：不然键盘收起来后 typing 还是 true，输入条会卡在半空。
    val focusManager = LocalFocusManager.current
    Box(
        modifier = Modifier
            .fillMaxSize()
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
            ) { focusManager.clearFocus() },
    ) {
        ImmersiveBackdrop()

        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(top = statusBarDp, bottom = if (typing) keyboardSlot else 0.dp),
        ) {
            ImmersiveTopBar(
                onNewSession = onNewSession,
                onCollapseToWindow = onCollapseToWindow,
                onToggleList = { showList = !showList },
                onClose = onCloseToBall,
            )

            // clipToBounds：长消息不能画到底部胶囊和输入条下面去。
            Box(modifier = Modifier.weight(1f).clipToBounds()) {
                if (state.messages.isEmpty()) {
                    ImmersiveEmpty(state)
                } else {
                    LazyColumn(
                        state = listState,
                        modifier = Modifier
                            .fillMaxSize()
                            .padding(horizontal = 18.dp),
                        verticalArrangement = Arrangement.spacedBy(18.dp),
                    ) {
                        items(state.messages.size) { index ->
                            val message = state.messages[index]
                            if (message.role == "user") {
                                UserMessage(message.text)
                            } else {
                                AssistantMessage(
                                    text = message.text,
                                    name = state.agents.firstOrNull { it.key == state.activeAgentKey }?.name
                                        ?: "助手",
                                )
                            }
                        }
                        if (state.activeBusy) {
                            item { BusyHint() }
                        }
                    }
                }
            }

            if (state.attachments.isNotEmpty()) {
                AttachmentTray(
                    attachments = state.attachments,
                    onRemove = onRemoveAttachment,
                )
            }

            CapabilityChips(
                onVoice = onVoiceStart,
                onOcr = onOcrStart,
                onAttach = onAttach,
            )

            ImmersiveComposer(
                state = state,
                onSend = onSend,
                onVoice = onVoiceStart,
                onTypingChanged = { focused ->
                    typing = focused
                    onTypingChanged(focused)
                },
                onKeyboardShow = { keyboard?.show() },
            )
        }

        if (showList) {
            ListSheet(
                state = state,
                onDismiss = { showList = false },
                onSelectAgent = {
                    // 选 Agent 只是换会话来源，浮层要留着让人接着选对话。
                    onSelectAgent(it)
                },
                onSelectSession = {
                    onSelectSession(it)
                    showList = false
                },
                onNewSession = {
                    onNewSession()
                    showList = false
                },
            )
        }
    }
}

/** 磨砂颗粒：96×96 白噪点，平铺后叠在玻璃上（Operit 同款做法）。 */
private fun noiseBitmap(size: Int = 96): ImageBitmap {
    val rnd = java.util.Random(0x5EED)
    val px = IntArray(size * size)
    for (i in px.indices) {
        val a = if (rnd.nextBoolean()) rnd.nextInt(26) else 0
        px[i] = (a shl 24) or 0x00FFFFFF
    }
    val bmp = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
    bmp.setPixels(px, 0, size, 0, 0, size, size)
    return bmp.asImageBitmap()
}

/**
 * 磨砂玻璃底，配方照 Operit 的 FloatingFullscreenScreen：
 *
 * 1. 窗口级 blurBehind 已经在 GlobalOverlayCompose 里打开（FLAG_BLUR_BEHIND +
 *    setBlurBehindRadius），SurfaceFlinger 负责把窗口后面的画面糊掉；
 * 2. 系统模糊生效时只压一层 ~22% 的黑罩 + 噪点，不再画任何不透明背景——
 *    之前那层 62%~81% 的深灰渐变把糊好的画面整个盖死了，看着就是一块灰板；
 * 3. 系统/机型不给跨窗口模糊时，退回不透明深灰蓝，避免露出清晰的 App 内容。
 */
@Composable
private fun ImmersiveBackdrop() {
    val context = LocalContext.current
    val canBlur = remember(context) {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
            context.getSystemService(WindowManager::class.java)?.isCrossWindowBlurEnabled == true
    }
    val noise = remember { noiseBitmap() }
    Canvas(modifier = Modifier.fillMaxSize()) {
        if (canBlur) {
            // 22% 黑罩：Operit 同款强度。再重就把模糊盖死，再轻白字压不住。
            drawRect(Color(0x38000000))
            // 顶部略深一点，给白色图标/文字一点底；底部轻微加深，压住输入条。
            drawRect(
                brush = Brush.verticalGradient(
                    0f to Color(0x1F000000),
                    0.35f to Color(0x00000000),
                    1f to Color(0x29000000),
                ),
            )
            // 右上冷光：玻璃的「厚度」感
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(Color(0x2E7C9AC4), Color.Transparent),
                    center = Offset(size.width * 0.88f, size.height * 0.08f),
                    radius = size.width * 0.85f,
                ),
                radius = size.width * 0.85f,
                center = Offset(size.width * 0.88f, size.height * 0.08f),
            )
            // 细噪点：磨砂玻璃的颗粒感，Operit 用 6% 白噪。
            drawRect(
                brush = ShaderBrush(ImageShader(noise, TileMode.Repeated, TileMode.Repeated)),
                alpha = 0.06f,
            )
        } else {
            // 没有跨窗口模糊：用不透明深灰蓝兜底，避免露出清晰的 App 内容显得脏。
            drawRect(
                brush = Brush.verticalGradient(
                    listOf(Color(0xFF4A5468), Color(0xFF39414F), Color(0xFF252B36)),
                ),
            )
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(Color(0x8A5C7A9E), Color.Transparent),
                    center = Offset(size.width * 0.88f, size.height * 0.10f),
                    radius = size.width * 0.9f,
                ),
                radius = size.width * 0.9f,
                center = Offset(size.width * 0.88f, size.height * 0.10f),
            )
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(Color(0x773B4A68), Color.Transparent),
                    center = Offset(size.width * 0.05f, size.height * 0.62f),
                    radius = size.width * 0.8f,
                ),
                radius = size.width * 0.8f,
                center = Offset(size.width * 0.05f, size.height * 0.62f),
            )
        }
    }
}

// ————————————————— 顶栏：纯白图标，无底色 —————————————————

@Composable
private fun ImmersiveTopBar(
    onNewSession: () -> Unit,
    onCollapseToWindow: () -> Unit,
    onToggleList: () -> Unit,
    onClose: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 26.dp, end = 18.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        GlyphButton(label = "新建对话", onClick = onNewSession) { PlusBubbleGlyph(22.dp) }
        Spacer(Modifier.weight(1f))
        GlyphButton(label = "收起为悬浮窗", onClick = onCollapseToWindow) { ChevronDownGlyph(20.dp) }
        Spacer(Modifier.width(14.dp))
        GlyphButton(label = "Agent 与会话", onClick = onToggleList) { ListBubbleGlyph(21.dp) }
        Spacer(Modifier.width(14.dp))
        GlyphButton(label = "关闭全局助手", onClick = onClose) { CloseGlyph(20.dp) }
    }
}

@Composable
private fun GlyphButton(
    label: String,
    size: Dp = 46.dp,
    onClick: () -> Unit,
    glyph: @Composable () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(size)
            .clip(CircleShape)
            .clickable(onClick = onClick)
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) { glyph() }
}

/** 白色圆角气泡 + 挖空的加号。 */
@Composable
private fun PlusBubbleGlyph(size: Dp) {
    Canvas(
        modifier = Modifier
            .size(size)
            .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen },
    ) {
        val w = this.size.width
        val h = this.size.height
        val bodyH = h * 0.80f
        val corner = w * 0.24f
        val body = Path().apply {
            addRoundRect(RoundRect(0f, 0f, w, bodyH, corner, corner))
            moveTo(w * 0.30f, bodyH - 1f)
            lineTo(w * 0.30f, h)
            lineTo(w * 0.48f, bodyH - 1f)
            close()
        }
        drawPath(body, RefIcon)
        val barW = w * 0.145f
        val armL = w * 0.60f
        drawRect(
            color = Color.Black,
            topLeft = Offset((w - armL) / 2f, bodyH / 2f - barW / 2f),
            size = Size(armL, barW),
            blendMode = BlendMode.Clear,
        )
        drawRect(
            color = Color.Black,
            topLeft = Offset(w / 2f - barW / 2f, bodyH / 2f - armL / 2f),
            size = Size(barW, armL),
            blendMode = BlendMode.Clear,
        )
    }
}

@Composable
private fun ChevronDownGlyph(size: Dp) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        drawLine(
            RefIcon,
            Offset(s.width * 0.06f, s.height * 0.32f),
            Offset(s.width * 0.5f, s.height * 0.72f),
            strokeWidth = s.width * 0.12f,
            cap = StrokeCap.Round,
        )
        drawLine(
            RefIcon,
            Offset(s.width * 0.5f, s.height * 0.72f),
            Offset(s.width * 0.94f, s.height * 0.32f),
            strokeWidth = s.width * 0.12f,
            cap = StrokeCap.Round,
        )
    }
}

/** 白色气泡 + 三条挖空横线。 */
@Composable
private fun ListBubbleGlyph(size: Dp) {
    Canvas(
        modifier = Modifier
            .size(size)
            .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen },
    ) {
        val s = this.size
        val w = s.width
        val h = s.height
        val bodyH = h * 0.84f
        val left = w * 0.04f
        val right = w * 0.96f
        val corner = w * 0.26f
        val path = Path().apply {
            addRoundRect(RoundRect(left, 0f, right, bodyH, corner, corner))
            moveTo(w * 0.26f, bodyH - 1f)
            lineTo(w * 0.26f, h)
            lineTo(w * 0.46f, bodyH - 1f)
            close()
        }
        drawPath(path, RefIcon)
        val lineW = w * 0.44f
        val lineLeft = (w - lineW) / 2f
        listOf(0.26f, 0.45f, 0.64f).forEach { fy ->
            drawRect(
                color = Color.Black,
                topLeft = Offset(lineLeft, bodyH * fy - h * 0.045f),
                size = Size(lineW, h * 0.09f),
                blendMode = BlendMode.Clear,
            )
        }
    }
}

@Composable
private fun CloseGlyph(size: Dp) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val stroke = s.width * 0.11f
        drawLine(RefIcon, Offset(0f, 0f), Offset(s.width, s.height), stroke, StrokeCap.Round)
        drawLine(RefIcon, Offset(s.width, 0f), Offset(0f, s.height), stroke, StrokeCap.Round)
    }
}

// ————————————————— 消息 —————————————————

@Composable
private fun AssistantMessage(text: String, name: String) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            AgentAvatar(34.dp)
            Spacer(Modifier.width(10.dp))
            Text(text = name, color = Color.White.copy(alpha = 0.66f), fontSize = 12.sp)
        }
        Spacer(Modifier.height(6.dp))
        Box(
            modifier = Modifier
                .padding(start = 44.dp, end = 26.dp)
                .clip(RoundedCornerShape(24.dp))
                .background(RefAiBubble)
                .padding(horizontal = 20.dp, vertical = 16.dp),
        ) {
            MarkdownText(text = text, color = RefBubbleInk, fontSize = 15.sp, lineHeight = 25.sp)
        }
    }
}

@Composable
private fun UserMessage(text: String) {
    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.End,
    ) {
        PersonGlyph(size = 22.dp, color = Color(0xFF2F6BE4))
        Spacer(Modifier.height(5.dp))
        Box(
            modifier = Modifier
                .widthIn(max = 250.dp)
                .clip(RoundedCornerShape(22.dp))
                .background(RefUserBubble)
                .padding(horizontal = 20.dp, vertical = 14.dp),
        ) {
            Text(text = text, color = RefBubbleInk, fontSize = 15.sp, lineHeight = 23.sp)
        }
    }
}

/**
 * 助手头像：直接取应用图标。
 * 不能用 painterResource(R.mipmap.ic_launcher)：自适应图标是 XML，运行时会抛
 * "Only VectorDrawables and rasterized asset types are supported" 直接崩溃。
 */
@Composable
private fun AgentAvatar(size: Dp) {
    val context = LocalContext.current
    val image = remember {
        runCatching {
            val drawable = context.packageManager.getApplicationIcon(context.packageName)
            val px = 96
            val bitmap = android.graphics.Bitmap.createBitmap(
                px,
                px,
                android.graphics.Bitmap.Config.ARGB_8888,
            )
            val canvas = android.graphics.Canvas(bitmap)
            drawable.setBounds(0, 0, px, px)
            drawable.draw(canvas)
            bitmap.asImageBitmap()
        }.getOrNull()
    }
    Box(
        modifier = Modifier
            .size(size)
            .clip(CircleShape)
            .background(Color.White.copy(alpha = 0.12f)),
        contentAlignment = Alignment.Center,
    ) {
        if (image != null) {
            Image(
                bitmap = image,
                contentDescription = null,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

/** 右侧那个蓝色人形图标。 */
@Composable
private fun PersonGlyph(size: Dp, color: Color) {
    Canvas(modifier = Modifier.size(size)) {
        val w = this.size.width
        val h = this.size.height
        drawCircle(color, radius = w * 0.20f, center = Offset(w / 2f, h * 0.22f))
        val body = Path().apply {
            moveTo(w * 0.10f, h * 0.98f)
            quadraticTo(w * 0.10f, h * 0.62f, w * 0.5f, h * 0.62f)
            quadraticTo(w * 0.90f, h * 0.62f, w * 0.90f, h * 0.98f)
            close()
        }
        drawPath(body, color)
    }
}

@Composable
private fun ImmersiveEmpty(state: OverlayUiState) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 26.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            "有什么可以帮你？",
            color = RefIcon,
            fontSize = 24.sp,
            // 背景是透的，后面可能是白卡片：给白字垫一层软阴影，亮底上也不至于糊掉。
            style = TextStyle(shadow = Shadow(Color(0x73000000), Offset(0f, 2f), 10f)),
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = state.agents.firstOrNull()?.let { "正在和 ${it.name} 对话" } ?: "可以直接派活给已连接的 Agent",
            color = Color.White.copy(alpha = 0.62f),
            fontSize = 13.sp,
            style = TextStyle(shadow = Shadow(Color(0x66000000), Offset(0f, 1f), 8f)),
        )
        if (state.activeBusy) {
            Spacer(Modifier.height(14.dp))
            BusyHint()
        }
    }
}

// ————————————————— 底部能力胶囊 —————————————————

@Composable
private fun CapabilityChips(onVoice: () -> Unit, onOcr: () -> Unit, onAttach: () -> Unit) {
    LazyRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 2.dp, bottom = 10.dp),
        contentPadding = PaddingValues(start = 40.dp, end = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        item {
            RefChip(onClick = onVoice) {
                MicIconGlyph(size = 17.dp, color = RefIcon)
                Spacer(Modifier.width(7.dp))
                ChipLabel("语音输入")
            }
        }
        item {
            RefChip(onClick = onOcr) {
                ScreenGlyph(size = 15.dp, color = RefIcon)
                Spacer(Modifier.width(7.dp))
                ChipLabel("屏幕内容")
            }
        }
        item {
            RefChip(onClick = onAttach) {
                AttachmentGlyph(size = 15.dp, color = RefIcon)
                Spacer(Modifier.width(7.dp))
                ChipLabel("附件")
            }
        }
    }
}

/** 执行中提示：呼吸小圆点 + 文案，替代之前完全没有反馈的空列表。 */
@Composable
private fun BusyHint() {
    val infinite = rememberInfiniteTransition(label = "busy")
    val alpha by infinite.animateFloat(
        initialValue = 0.30f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(900),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "busyAlpha",
    )
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier
                .size(6.dp)
                .clip(CircleShape)
                .background(Color(0xFF9CC0FF).copy(alpha = alpha)),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = "正在执行…",
            color = Color.White.copy(alpha = 0.72f),
            fontSize = 13.sp,
            style = TextStyle(shadow = Shadow(Color(0x59000000), Offset(0f, 1f), 6f)),
        )
    }
}

/** 已选附件：显示在输入条上方，点一下移除。 */
@Composable
private fun AttachmentTray(attachments: List<AttachmentUi>, onRemove: (Int) -> Unit) {
    LazyRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 40.dp, end = 20.dp, bottom = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(attachments.size) { index ->
            val item = attachments[index]
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(50))
                    .background(Color.White.copy(alpha = 0.16f))
                    .clickable { onRemove(index) }
                    .padding(horizontal = 12.dp, vertical = 7.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                AttachmentGlyph(size = 13.dp, color = RefIcon)
                Spacer(Modifier.width(6.dp))
                Text(
                    text = item.name,
                    color = RefIcon,
                    fontSize = 11.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.widthIn(max = 150.dp),
                )
                Spacer(Modifier.width(6.dp))
                Text("×", color = Color.White.copy(alpha = 0.6f), fontSize = 12.sp)
            }
        }
    }
}

@Composable
private fun RefChip(onClick: () -> Unit, content: @Composable () -> Unit) {
    Row(
        modifier = Modifier
            .height(34.dp)
            .clip(RoundedCornerShape(50))
            .background(RefChipBg)
            .border(1.dp, RefChipBorder, RoundedCornerShape(50))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) { content() }
}

@Composable
private fun ChipLabel(text: String) {
    Text(text, color = RefIcon, fontSize = 12.sp, maxLines = 1)
}

// ————————————————— 输入条 —————————————————

@Composable
private fun ImmersiveComposer(
    state: OverlayUiState,
    onSend: (String) -> Unit,
    onVoice: () -> Unit,
    onTypingChanged: (Boolean) -> Unit,
    onKeyboardShow: () -> Unit,
) {
    val keyboard = LocalSoftwareKeyboardController.current
    val interaction = remember { MutableInteractionSource() }
    LaunchedEffect(interaction) {
        interaction.interactions.collect { event ->
            if (event is PressInteraction.Release) keyboard?.show()
        }
    }
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 34.dp, end = 34.dp, bottom = 20.dp),
    ) {
        // 参考图输入条外面那圈青绿光晕：单独一层模糊渐变，系统 shadow 做不出这个颜色。
        Box(
            modifier = Modifier
                .matchParentSize()
                .blur(26.dp, androidx.compose.ui.draw.BlurredEdgeTreatment.Unbounded)
                .background(
                    brush = Brush.horizontalGradient(
                        listOf(
                            Color(0x9934D399),
                            Color(0x9938BDF8),
                            Color(0x9960A5FA),
                        ),
                    ),
                    shape = RoundedCornerShape(50),
                ),
        )
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(58.dp)
                .shadow(
                    elevation = 26.dp,
                    shape = RoundedCornerShape(50),
                    ambientColor = Color(0xFF34D399).copy(alpha = 0.55f),
                    spotColor = Color(0xFF38BDF8).copy(alpha = 0.55f),
                )
                .clip(RoundedCornerShape(50))
                .background(Color.White)
                .padding(start = 18.dp, end = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(30.dp)
                    .clip(CircleShape)
                    .clickable(onClick = onVoice),
                contentAlignment = Alignment.Center,
            ) { MicIconGlyph(size = 20.dp, color = Color(0xFF6B7280)) }
            Spacer(Modifier.width(8.dp))
            BasicTextField(
                value = state.input,
                onValueChange = { state.input = it },
                singleLine = true,
                interactionSource = interaction,
                textStyle = TextStyle(color = RefBubbleInk, fontSize = 15.sp),
                cursorBrush = SolidColor(Color(0xFF2F6BE4)),
                modifier = Modifier
                    .weight(1f)
                    .onFocusChanged {
                        onTypingChanged(it.isFocused)
                        if (it.isFocused) onKeyboardShow()
                    },
                decorationBox = { inner ->
                    if (state.input.isEmpty()) {
                        Text("输入或者长按语音", color = RefMutedText, fontSize = 15.sp)
                    }
                    inner()
                },
            )
            Box(
                modifier = Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .clickable {
                        val text = state.input.trim()
                        if (text.isNotEmpty()) {
                            state.input = ""
                            onSend(text)
                        }
                    },
                contentAlignment = Alignment.Center,
            ) { SendGlyph(24.dp) }
        }
    }
}

@Composable
private fun SendGlyph(size: Dp) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val path = Path().apply {
            moveTo(s.width * 0.10f, s.height * 0.14f)
            lineTo(s.width * 0.94f, s.height * 0.50f)
            lineTo(s.width * 0.10f, s.height * 0.86f)
            lineTo(s.width * 0.26f, s.height * 0.50f)
            close()
        }
        drawPath(path, RefSend)
    }
}

// ————————————————— 列表浮层 —————————————————

@Composable
private fun ListSheet(
    state: OverlayUiState,
    onDismiss: () -> Unit,
    onSelectAgent: (String) -> Unit,
    onSelectSession: (String) -> Unit,
    onNewSession: () -> Unit,
) {
    val sheetShape = RoundedCornerShape(26.dp)
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0x330B1220))
            .clickable(onClick = onDismiss),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 96.dp, start = 16.dp, end = 16.dp)
                .shadow(22.dp, sheetShape, clip = false, ambientColor = Color(0x330B1220), spotColor = Color(0x330B1220))
                .clip(sheetShape)
                // 亮色玻璃：整层沉浸页是浅色的，历史浮层再用深色会很突兀。
                .background(Color(0xF2FFFFFF))
                .border(1.dp, Color.White.copy(alpha = 0.85f), sheetShape)
                .clickable(enabled = false) {}
                .padding(horizontal = 16.dp, vertical = 14.dp)
                .heightIn(max = 580.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("历史对话", color = SheetInk, fontSize = 15.sp, fontWeight = FontWeight.Medium)
                Spacer(Modifier.weight(1f))
                Text(
                    text = "＋ 新对话",
                    color = Color.White,
                    fontSize = 12.sp,
                    modifier = Modifier
                        .clip(RoundedCornerShape(50))
                        .background(Color(0xFF2F6BE4))
                        .clickable(onClick = onNewSession)
                        .padding(horizontal = 12.dp, vertical = 6.dp),
                )
            }

            Spacer(Modifier.height(14.dp))
            Text("Agent", color = SheetMuted, fontSize = 10.sp)
            Spacer(Modifier.height(8.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(state.agents.size) { index ->
                    val agent = state.agents[index]
                    val selected = agent.key == state.activeAgentKey
                    Text(
                        text = agent.name.ifBlank { agent.agentId.take(8) },
                        color = if (selected) Color.White else SheetInk,
                        fontSize = 12.sp,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier
                            .clip(RoundedCornerShape(50))
                            .background(if (selected) Color(0xFF2F6BE4) else Color(0xFFF1F5FB))
                            .border(
                                1.dp,
                                if (selected) Color.Transparent else Color(0xFFE3E9F2),
                                RoundedCornerShape(50),
                            )
                            .clickable { onSelectAgent(agent.key) }
                            .padding(horizontal = 14.dp, vertical = 8.dp),
                    )
                }
            }

            Spacer(Modifier.height(16.dp))
            Text("对话", color = SheetMuted, fontSize = 10.sp)
            Spacer(Modifier.height(8.dp))
            LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                items(state.sessions.size) { index ->
                    val session = state.sessions[index]
                    val selected = session.sessionId == state.currentSessionId
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(14.dp))
                            .background(if (selected) Color(0xFFEAF1FF) else Color(0xFFF6F8FC))
                            .border(
                                1.dp,
                                if (selected) Color(0x662F6BE4) else Color.Transparent,
                                RoundedCornerShape(14.dp),
                            )
                            .clickable { onSelectSession(session.sessionId) }
                            .padding(horizontal = 12.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            modifier = Modifier
                                .size(6.dp)
                                .clip(CircleShape)
                                .background(if (selected) Color(0xFF2F6BE4) else Color(0xFFB8C4D6)),
                        )
                        Spacer(Modifier.width(10.dp))
                        Text(
                            text = session.summary.ifBlank { session.sessionId.take(8) },
                            color = if (selected) Color(0xFF17325F) else SheetInk,
                            fontSize = 13.sp,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        if (selected) {
                            Spacer(Modifier.width(8.dp))
                            Text("当前", color = Color(0xFF2F6BE4), fontSize = 10.sp)
                        }
                    }
                }
            }
        }
    }
}

private val SheetInk = Color(0xFF14213A)
private val SheetMuted = Color(0xFF7C8AA0)
