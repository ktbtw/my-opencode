package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * 轻量 Markdown 渲染：Agent 的回复是 markdown（标题 / 列表 / 代码块 / 表格 / 行内代码），
 * 之前直接当纯文本塞进气泡，反引号和竖线全露出来了。
 *
 * 只实现聊天里真正会出现的子集，不引第三方库：
 * 标题、段落、无序/有序列表、引用、分隔线、围栏代码块、表格、行内 **粗体** / *斜体* / `代码` / [链接](url)。
 */

private sealed interface MdBlock {
    data class Heading(val level: Int, val text: String) : MdBlock
    data class Paragraph(val text: String) : MdBlock
    data class Code(val lang: String, val code: String) : MdBlock
    data class Quote(val text: String) : MdBlock
    data class Bullet(val items: List<String>) : MdBlock
    data class Ordered(val items: List<Pair<Int, String>>) : MdBlock
    data class Table(val header: List<String>, val rows: List<List<String>>) : MdBlock
    data object Divider : MdBlock
}

private fun parseMarkdown(source: String): List<MdBlock> {
    val lines = source.replace("\r\n", "\n").split('\n')
    val blocks = mutableListOf<MdBlock>()
    var i = 0
    val paragraph = StringBuilder()

    fun flushParagraph() {
        if (paragraph.isNotBlank()) {
            blocks.add(MdBlock.Paragraph(paragraph.toString().trim()))
            paragraph.setLength(0)
        }
    }

    while (i < lines.size) {
        val raw = lines[i]
        val line = raw.trimEnd()
        val trimmed = line.trim()

        // 围栏代码块
        if (trimmed.startsWith("```")) {
            flushParagraph()
            val lang = trimmed.removePrefix("```").trim()
            val body = StringBuilder()
            i++
            while (i < lines.size && !lines[i].trim().startsWith("```")) {
                body.appendLine(lines[i])
                i++
            }
            if (i < lines.size) i++ // 跳过结束围栏
            blocks.add(MdBlock.Code(lang, body.toString().trimEnd()))
            continue
        }

        // 分隔线
        if (trimmed.matches(Regex("^([-*_])\\1{2,}$"))) {
            flushParagraph()
            blocks.add(MdBlock.Divider)
            i++
            continue
        }

        // 标题
        val heading = Regex("^(#{1,6})\\s+(.*)$").find(trimmed)
        if (heading != null) {
            flushParagraph()
            blocks.add(MdBlock.Heading(heading.groupValues[1].length, heading.groupValues[2].trim()))
            i++
            continue
        }

        // 表格：当前行含 |，下一行是 |---| 分隔
        if (trimmed.startsWith("|") && i + 1 < lines.size &&
            lines[i + 1].trim().matches(Regex("^\\|?\\s*:?-{2,}:?\\s*(\\|\\s*:?-{2,}:?\\s*)+\\|?$"))
        ) {
            flushParagraph()
            val header = splitTableRow(trimmed)
            i += 2
            val rows = mutableListOf<List<String>>()
            while (i < lines.size && lines[i].trim().startsWith("|")) {
                rows.add(splitTableRow(lines[i].trim()))
                i++
            }
            blocks.add(MdBlock.Table(header, rows))
            continue
        }

        // 引用
        if (trimmed.startsWith(">")) {
            flushParagraph()
            val quote = StringBuilder()
            while (i < lines.size && lines[i].trim().startsWith(">")) {
                if (quote.isNotEmpty()) quote.append('\n')
                quote.append(lines[i].trim().removePrefix(">").trimStart())
                i++
            }
            blocks.add(MdBlock.Quote(quote.toString()))
            continue
        }

        // 无序列表
        if (trimmed.matches(Regex("^[-*+]\\s+.*"))) {
            flushParagraph()
            val items = mutableListOf<String>()
            while (i < lines.size && lines[i].trim().matches(Regex("^[-*+]\\s+.*"))) {
                items.add(lines[i].trim().replaceFirst(Regex("^[-*+]\\s+"), ""))
                i++
            }
            blocks.add(MdBlock.Bullet(items))
            continue
        }

        // 有序列表
        if (trimmed.matches(Regex("^\\d+[.)]\\s+.*"))) {
            flushParagraph()
            val items = mutableListOf<Pair<Int, String>>()
            while (i < lines.size && lines[i].trim().matches(Regex("^\\d+[.)]\\s+.*"))) {
                val item = lines[i].trim()
                val number = item.takeWhile { it.isDigit() }.toIntOrNull() ?: (items.size + 1)
                items.add(number to item.replaceFirst(Regex("^\\d+[.)]\\s+"), ""))
                i++
            }
            blocks.add(MdBlock.Ordered(items))
            continue
        }

        // 空行 = 段落分隔
        if (trimmed.isEmpty()) {
            flushParagraph()
            i++
            continue
        }

        if (paragraph.isNotEmpty()) paragraph.append('\n')
        paragraph.append(trimmed)
        i++
    }
    flushParagraph()
    return blocks
}

private fun splitTableRow(row: String): List<String> =
    row.trim().removePrefix("|").removeSuffix("|").split('|').map { it.trim() }

/** 行内标记：**粗体**、*斜体*、`代码`、[文字](链接)。 */
private fun inlineMarkdown(text: String, codeBg: Color, linkColor: Color): AnnotatedString =
    buildAnnotatedString {
        var i = 0
        while (i < text.length) {
            when {
                text.startsWith("**", i) -> {
                    val end = text.indexOf("**", i + 2)
                    if (end > 0) {
                        pushStyle(SpanStyle(fontWeight = FontWeight.SemiBold))
                        append(text.substring(i + 2, end))
                        pop()
                        i = end + 2
                    } else {
                        append(text[i]); i++
                    }
                }
                text[i] == '`' -> {
                    val end = text.indexOf('`', i + 1)
                    if (end > 0) {
                        pushStyle(
                            SpanStyle(
                                fontFamily = FontFamily.Monospace,
                                background = codeBg,
                                fontSize = 13.sp,
                            ),
                        )
                        append(text.substring(i + 1, end))
                        pop()
                        i = end + 1
                    } else {
                        append(text[i]); i++
                    }
                }
                text[i] == '*' && i + 1 < text.length && text[i + 1] != '*' -> {
                    val end = text.indexOf('*', i + 1)
                    if (end > 0) {
                        pushStyle(SpanStyle(fontStyle = FontStyle.Italic))
                        append(text.substring(i + 1, end))
                        pop()
                        i = end + 1
                    } else {
                        append(text[i]); i++
                    }
                }
                text[i] == '[' -> {
                    val close = text.indexOf(']', i + 1)
                    val open = if (close > 0) text.indexOf('(', close) else -1
                    val end = if (open == close + 1) text.indexOf(')', open) else -1
                    if (close > 0 && end > 0) {
                        pushStyle(SpanStyle(color = linkColor, textDecoration = TextDecoration.Underline))
                        append(text.substring(i + 1, close))
                        pop()
                        i = end + 1
                    } else {
                        append(text[i]); i++
                    }
                }
                else -> {
                    append(text[i]); i++
                }
            }
        }
    }

@Composable
fun MarkdownText(
    text: String,
    color: Color,
    fontSize: TextUnit = 15.sp,
    lineHeight: TextUnit = 24.sp,
    modifier: Modifier = Modifier,
) {
    val blocks = remember(text) { parseMarkdown(text) }
    val codeBg = Color(0x140F172A)
    val codeBlockBg = Color(0xFF111827)
    val codeBlockInk = Color(0xFFE5E7EB)
    val linkColor = Color(0xFF2F6BE4)

    Column(modifier = modifier) {
        blocks.forEachIndexed { index, block ->
            when (block) {
                is MdBlock.Heading -> {
                    val size = when (block.level) {
                        1 -> (fontSize.value + 5).sp
                        2 -> (fontSize.value + 3).sp
                        3 -> (fontSize.value + 1).sp
                        else -> fontSize
                    }
                    Text(
                        text = inlineMarkdown(block.text, codeBg, linkColor),
                        color = color,
                        fontSize = size,
                        fontWeight = FontWeight.SemiBold,
                        lineHeight = (size.value + 8).sp,
                    )
                }

                is MdBlock.Paragraph -> Text(
                    text = inlineMarkdown(block.text, codeBg, linkColor),
                    color = color,
                    fontSize = fontSize,
                    lineHeight = lineHeight,
                )

                is MdBlock.Code -> Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(10.dp))
                        .background(codeBlockBg)
                        .horizontalScroll(rememberScrollState())
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    Text(
                        text = block.code,
                        color = codeBlockInk,
                        fontSize = 12.sp,
                        lineHeight = 18.sp,
                        fontFamily = FontFamily.Monospace,
                    )
                }

                is MdBlock.Quote -> Row(modifier = Modifier.fillMaxWidth()) {
                    Box(
                        modifier = Modifier
                            .width(3.dp)
                            .height(20.dp)
                            .background(color.copy(alpha = 0.25f), RoundedCornerShape(2.dp)),
                    )
                    Spacer(Modifier.width(10.dp))
                    Text(
                        text = inlineMarkdown(block.text, codeBg, linkColor),
                        color = color.copy(alpha = 0.8f),
                        fontSize = fontSize,
                        lineHeight = lineHeight,
                    )
                }

                is MdBlock.Bullet -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    block.items.forEach { item ->
                        Row(modifier = Modifier.fillMaxWidth()) {
                            Text("•", color = color.copy(alpha = 0.7f), fontSize = fontSize)
                            Spacer(Modifier.width(8.dp))
                            Text(
                                text = inlineMarkdown(item, codeBg, linkColor),
                                color = color,
                                fontSize = fontSize,
                                lineHeight = lineHeight,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                }

                is MdBlock.Ordered -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    block.items.forEach { (number, item) ->
                        Row(modifier = Modifier.fillMaxWidth()) {
                            Text("$number.", color = color.copy(alpha = 0.7f), fontSize = fontSize)
                            Spacer(Modifier.width(8.dp))
                            Text(
                                text = inlineMarkdown(item, codeBg, linkColor),
                                color = color,
                                fontSize = fontSize,
                                lineHeight = lineHeight,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                }

                is MdBlock.Table -> Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(10.dp))
                        .background(codeBg)
                        .padding(10.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Row(Modifier.fillMaxWidth()) {
                        block.header.forEach { cell ->
                            Text(
                                text = inlineMarkdown(cell, codeBg, linkColor),
                                color = color,
                                fontSize = 12.sp,
                                fontWeight = FontWeight.SemiBold,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                    block.rows.forEach { row ->
                        Row(Modifier.fillMaxWidth()) {
                            row.forEach { cell ->
                                Text(
                                    text = inlineMarkdown(cell, codeBg, linkColor),
                                    color = color.copy(alpha = 0.85f),
                                    fontSize = 12.sp,
                                    modifier = Modifier.weight(1f),
                                )
                            }
                        }
                    }
                }

                MdBlock.Divider -> Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(1.dp)
                        .background(color.copy(alpha = 0.12f)),
                )
            }
            if (index != blocks.lastIndex) Spacer(Modifier.height(10.dp))
        }
    }
}
