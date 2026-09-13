import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';

import '../../../core/storage/app_storage.dart';
import '../../../core/theme/app_colors.dart';

class MobileOnboardingPage extends StatefulWidget {
  const MobileOnboardingPage({super.key});

  @override
  State<MobileOnboardingPage> createState() => _MobileOnboardingPageState();
}

class _MobileOnboardingPageState extends State<MobileOnboardingPage> {
  static const _slides = [
    _OnboardingSlide(
      kicker: 'CODE CONTROL / 01',
      title: '六个 Agent，\n一套工作台。',
      description: '把编码与逆向分析交给对应的专家，打开项目就能进入状态。',
      kind: _SlideKind.agents,
    ),
    _OnboardingSlide(
      kicker: 'CODE CONTROL / 02',
      title: '从线索到\n可交付结果。',
      description: '工具链、Skill、MCP 与会话上下文协同工作，让复杂任务保持清晰。',
      kind: _SlideKind.workflow,
    ),
    _OnboardingSlide(
      kicker: 'CODE CONTROL / 03',
      title: '你的下一步，\n现在开始。',
      description: '保留任务进度、交付文件与历史会话，随时回到正在进行的工作。',
      kind: _SlideKind.delivery,
    ),
  ];

  final _pageController = PageController();
  late final _FrameClock _frameClock;
  int _page = 0;

  @override
  void initState() {
    super.initState();
    _frameClock = _FrameClock()..start();
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
  }

  @override
  void dispose() {
    _pageController.dispose();
    _frameClock.dispose();
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    super.dispose();
  }

  Future<void> _finish(String route) async {
    await AppStorage.completeMobileOnboarding();
    if (!mounted) return;
    context.go(route);
  }

  String get _authRoute => AppStorage.isLoggedIn() ? '/startup' : '/login';

  void _next() {
    if (_page == _slides.length - 1) {
      _finish(_authRoute);
      return;
    }
    _pageController.nextPage(
      duration: const Duration(milliseconds: 420),
      curve: Curves.easeOutCubic,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: SystemUiOverlayStyle.dark,
      child: Scaffold(
        backgroundColor: _OnboardingColors.ink,
        body: Stack(
          fit: StackFit.expand,
          children: [
            CustomPaint(painter: _OnboardingBackdrop()),
            PageView.builder(
              controller: _pageController,
              itemCount: _slides.length,
              onPageChanged: (value) => setState(() => _page = value),
              itemBuilder: (context, index) =>
                  _SlideView(slide: _slides[index]),
            ),
            _TopBar(page: _page, pageCount: _slides.length, clock: _frameClock),
            _BottomControls(
              page: _page,
              pageCount: _slides.length,
              onNext: _next,
              onLogin: () => _finish('/login'),
              onRegister: () => _finish('/register'),
            ),
          ],
        ),
      ),
    );
  }
}

enum _SlideKind { agents, workflow, delivery }

class _OnboardingSlide {
  final String kicker;
  final String title;
  final String description;
  final _SlideKind kind;

  const _OnboardingSlide({
    required this.kicker,
    required this.title,
    required this.description,
    required this.kind,
  });
}

class _OnboardingColors {
  static const ink = AppColors.background;
  static const panel = Color(0xE6FFFFFF);
  static const panelStrong = Color(0xF7FFFFFF);
  static const line = AppColors.border;
  static const cyan = AppColors.primary;
  static const lime = AppColors.statusOnline;
  static const text = AppColors.textPrimary;
  static const muted = AppColors.textSecondary;
  static const orange = AppColors.statusWarning;
}

class _TopBar extends StatelessWidget {
  final int page;
  final int pageCount;
  final _FrameClock clock;

  const _TopBar({
    required this.page,
    required this.pageCount,
    required this.clock,
  });

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(22, 12, 22, 0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _BrandMark(clock: clock),
            const Spacer(),
            Column(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Text(
                  '0${page + 1} / 0$pageCount',
                  style: const TextStyle(
                    color: _OnboardingColors.text,
                    fontFamily: 'monospace',
                    fontSize: 11,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 1.2,
                  ),
                ),
                const SizedBox(height: 8),
                SizedBox(
                  width: 66,
                  child: Row(
                    children: List.generate(
                      pageCount,
                      (index) => Expanded(
                        child: AnimatedContainer(
                          duration: const Duration(milliseconds: 220),
                          margin: EdgeInsets.only(left: index == 0 ? 0 : 4),
                          height: 2,
                          color: index <= page
                              ? _OnboardingColors.cyan
                              : _OnboardingColors.line,
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _BrandMark extends StatelessWidget {
  final _FrameClock clock;

  const _BrandMark({required this.clock});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 160,
      height: 58,
      child: CustomPaint(painter: _BrandMarkPainter(clock: clock)),
    );
  }
}

class _BrandMarkPainter extends CustomPainter {
  final _FrameClock clock;
  late final TextPainter _fillPainter;
  late final TextPainter _outlinePainter;

  _BrandMarkPainter({required this.clock}) : super(repaint: clock) {
    _fillPainter = TextPainter(
      text: const TextSpan(
        text: '码控',
        style: TextStyle(
          color: _OnboardingColors.text,
          fontFamily: 'monospace',
          fontSize: 35,
          fontWeight: FontWeight.w900,
          height: 1,
        ),
      ),
      textDirection: TextDirection.ltr,
    )..layout();
    _outlinePainter = TextPainter(
      text: TextSpan(
        text: '码控',
        style: TextStyle(
          fontFamily: 'monospace',
          fontSize: 35,
          fontWeight: FontWeight.w900,
          height: 1,
          foreground: Paint()
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.4
            ..color = _OnboardingColors.cyan,
        ),
      ),
      textDirection: TextDirection.ltr,
    )..layout();
  }

  @override
  void paint(Canvas canvas, Size size) {
    final reveal = (clock.frame / 18).clamp(0.0, 1.0);
    final jitter = reveal < 1 ? ((clock.frame % 3) - 1).toDouble() : 0.0;
    final startX = (size.width - _fillPainter.width) / 2;
    final clipWidth = _fillPainter.width * reveal;
    canvas.save();
    canvas.clipRect(Rect.fromLTWH(startX, 0, clipWidth + 4, size.height));
    final textOffset = Offset(startX + jitter, 2 + jitter);
    _outlinePainter.paint(canvas, textOffset.translate(-1.5, 0));
    _fillPainter.paint(canvas, textOffset);
    canvas.restore();

    final underlineProgress = (reveal * 1.15).clamp(0.0, 1.0);
    final underlinePaint = Paint()
      ..color = _OnboardingColors.cyan.withValues(alpha: 0.7)
      ..strokeWidth = 2
      ..strokeCap = StrokeCap.square;
    final underlineY = size.height - 5;
    canvas.drawLine(
      Offset(startX, underlineY),
      Offset(startX + _fillPainter.width * underlineProgress, underlineY),
      underlinePaint,
    );
    if (reveal > 0.1) {
      final cornerPaint = Paint()
        ..color = _OnboardingColors.lime.withValues(alpha: 0.85)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5;
      final cornerX = startX + _fillPainter.width * underlineProgress + 5;
      canvas.drawLine(
        Offset(cornerX, underlineY - 7),
        Offset(cornerX, underlineY + 1),
        cornerPaint,
      );
      canvas.drawLine(
        Offset(cornerX, underlineY + 1),
        Offset(cornerX + 7, underlineY + 1),
        cornerPaint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _BrandMarkPainter oldDelegate) => false;
}

class _SlideView extends StatelessWidget {
  final _OnboardingSlide slide;

  const _SlideView({required this.slide});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          22,
          102,
          22,
          constraints.maxHeight > 700 ? 180 : 162,
        ),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: constraints.maxHeight - 264),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const SizedBox(height: 8),
              Text(
                slide.kicker,
                style: const TextStyle(
                  color: _OnboardingColors.cyan,
                  fontFamily: 'monospace',
                  fontSize: 11,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 1.1,
                ),
              ),
              const SizedBox(height: 18),
              Text(
                slide.title,
                style: const TextStyle(
                  color: _OnboardingColors.text,
                  fontFamily: 'monospace',
                  fontSize: 31,
                  fontWeight: FontWeight.w800,
                  height: 1.13,
                ),
              ),
              const SizedBox(height: 16),
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 355),
                child: Text(
                  slide.description,
                  style: const TextStyle(
                    color: _OnboardingColors.muted,
                    fontSize: 14,
                    height: 1.55,
                  ),
                ),
              ),
              const SizedBox(height: 28),
              switch (slide.kind) {
                _SlideKind.agents => const _AgentStage(),
                _SlideKind.workflow => const _WorkflowStage(),
                _SlideKind.delivery => const _DeliveryStage(),
              },
            ],
          ),
        ),
      ),
    );
  }
}

class _AgentStage extends StatelessWidget {
  const _AgentStage();

  static const _agents = [
    ('编码 Agent', Icons.code_rounded, 'BUILD'),
    ('逆向专家-安卓', Icons.android_rounded, 'APK'),
    ('逆向专家-ios', Icons.phone_iphone_rounded, 'IPA'),
    ('逆向专家-mac', Icons.laptop_mac_rounded, 'MACH-O'),
    ('逆向专家-web', Icons.language_rounded, 'WEB'),
    ('逆向专家-windows', Icons.window_rounded, 'PE'),
  ];

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _StageLabel(label: 'AVAILABLE AGENTS'),
        const SizedBox(height: 12),
        GridView.builder(
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          itemCount: _agents.length,
          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 2,
            mainAxisSpacing: 8,
            crossAxisSpacing: 8,
            mainAxisExtent: 52,
          ),
          itemBuilder: (context, index) {
            final agent = _agents[index];
            return Container(
              padding: const EdgeInsets.symmetric(horizontal: 10),
              decoration: BoxDecoration(
                color: _OnboardingColors.panel,
                border: Border.all(color: _OnboardingColors.line),
              ),
              child: Row(
                children: [
                  Icon(agent.$2, color: _OnboardingColors.cyan, size: 17),
                  const SizedBox(width: 7),
                  Expanded(
                    child: FittedBox(
                      fit: BoxFit.scaleDown,
                      alignment: Alignment.centerLeft,
                      child: Text(
                        agent.$1,
                        maxLines: 1,
                        style: const TextStyle(
                          color: _OnboardingColors.text,
                          fontSize: 11.5,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 5),
                  Text(
                    agent.$3,
                    style: const TextStyle(
                      color: _OnboardingColors.lime,
                      fontFamily: 'monospace',
                      fontSize: 8,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ],
              ),
            );
          },
        ),
      ],
    );
  }
}

class _WorkflowStage extends StatelessWidget {
  const _WorkflowStage();

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _StageLabel(label: 'TASK PIPELINE'),
        const SizedBox(height: 12),
        Container(
          padding: const EdgeInsets.fromLTRB(16, 15, 16, 13),
          decoration: BoxDecoration(
            color: _OnboardingColors.panelStrong,
            border: Border.all(color: _OnboardingColors.line),
          ),
          child: Column(
            children: [
              const _PipelineRow(
                index: '01',
                title: '理解任务',
                detail: 'context / session',
                color: _OnboardingColors.cyan,
              ),
              const _PipelineConnector(),
              const _PipelineRow(
                index: '02',
                title: '调用能力',
                detail: 'skill + mcp + tools',
                color: _OnboardingColors.orange,
              ),
              const _PipelineConnector(),
              const _PipelineRow(
                index: '03',
                title: '确认交付',
                detail: 'artifact / history',
                color: _OnboardingColors.lime,
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        const Row(
          children: [
            _Metric(value: '24/7', label: 'READY'),
            SizedBox(width: 8),
            _Metric(value: '∞', label: 'CONTEXT'),
            SizedBox(width: 8),
            _Metric(value: '1', label: 'WORKSPACE'),
          ],
        ),
      ],
    );
  }
}

class _DeliveryStage extends StatelessWidget {
  const _DeliveryStage();

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _StageLabel(label: 'LAST MILE / READY'),
        const SizedBox(height: 12),
        Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: _OnboardingColors.panelStrong,
            border: Border.all(
              color: _OnboardingColors.cyan.withValues(alpha: 0.45),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Row(
                children: [
                  _PulseDot(),
                  SizedBox(width: 9),
                  Expanded(
                    child: Text(
                      'AGENT SESSION ACTIVE',
                      style: TextStyle(
                        color: _OnboardingColors.text,
                        fontFamily: 'monospace',
                        fontSize: 11,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  Text(
                    '100%',
                    style: TextStyle(
                      color: _OnboardingColors.lime,
                      fontFamily: 'monospace',
                      fontSize: 11,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 18),
              const ClipRRect(
                borderRadius: BorderRadius.all(Radius.circular(1)),
                child: LinearProgressIndicator(
                  value: 1,
                  minHeight: 3,
                  backgroundColor: _OnboardingColors.line,
                  color: _OnboardingColors.cyan,
                ),
              ),
              const SizedBox(height: 17),
              const Text(
                'deliverable.zip',
                style: TextStyle(
                  color: _OnboardingColors.cyan,
                  fontFamily: 'monospace',
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 7),
              const Text(
                'history synced  ·  workspace preserved',
                style: TextStyle(
                  color: _OnboardingColors.muted,
                  fontFamily: 'monospace',
                  fontSize: 10,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _StageLabel extends StatelessWidget {
  final String label;

  const _StageLabel({required this.label});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Container(width: 5, height: 5, color: _OnboardingColors.lime),
        const SizedBox(width: 8),
        Text(
          label,
          style: const TextStyle(
            color: _OnboardingColors.muted,
            fontFamily: 'monospace',
            fontSize: 9,
            fontWeight: FontWeight.w700,
            letterSpacing: 1.2,
          ),
        ),
      ],
    );
  }
}

class _PipelineRow extends StatelessWidget {
  final String index;
  final String title;
  final String detail;
  final Color color;

  const _PipelineRow({
    required this.index,
    required this.title,
    required this.detail,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(
          index,
          style: TextStyle(
            color: color,
            fontFamily: 'monospace',
            fontSize: 10,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(width: 13),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: const TextStyle(
                  color: _OnboardingColors.text,
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 3),
              Text(
                detail,
                style: const TextStyle(
                  color: _OnboardingColors.muted,
                  fontFamily: 'monospace',
                  fontSize: 9,
                ),
              ),
            ],
          ),
        ),
        Icon(Icons.arrow_forward_rounded, size: 16, color: color),
      ],
    );
  }
}

class _PipelineConnector extends StatelessWidget {
  const _PipelineConnector();

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.only(left: 22, top: 5, bottom: 5),
        width: 1,
        height: 16,
        color: _OnboardingColors.line,
      ),
    );
  }
}

class _Metric extends StatelessWidget {
  final String value;
  final String label;

  const _Metric({required this.value, required this.label});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 11),
        decoration: const BoxDecoration(
          color: _OnboardingColors.panel,
          border: Border.fromBorderSide(
            BorderSide(color: _OnboardingColors.line),
          ),
        ),
        child: Column(
          children: [
            Text(
              value,
              style: const TextStyle(
                color: _OnboardingColors.text,
                fontFamily: 'monospace',
                fontSize: 15,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 3),
            Text(
              label,
              style: const TextStyle(
                color: _OnboardingColors.muted,
                fontFamily: 'monospace',
                fontSize: 8,
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PulseDot extends StatelessWidget {
  const _PulseDot();

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 8,
      height: 8,
      decoration: const BoxDecoration(
        color: _OnboardingColors.lime,
        shape: BoxShape.circle,
      ),
    );
  }
}

class _BottomControls extends StatelessWidget {
  final int page;
  final int pageCount;
  final VoidCallback onNext;
  final VoidCallback onLogin;
  final VoidCallback onRegister;

  const _BottomControls({
    required this.page,
    required this.pageCount,
    required this.onNext,
    required this.onLogin,
    required this.onRegister,
  });

  @override
  Widget build(BuildContext context) {
    final isLast = page == pageCount - 1;
    return Align(
      alignment: Alignment.bottomCenter,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(22, 12, 22, 18),
          child: AnimatedSwitcher(
            duration: const Duration(milliseconds: 240),
            child: isLast
                ? Row(
                    key: const ValueKey('auth-actions'),
                    children: [
                      Expanded(
                        child: OutlinedButton(
                          onPressed: onLogin,
                          style: OutlinedButton.styleFrom(
                            foregroundColor: _OnboardingColors.text,
                            side: const BorderSide(
                              color: _OnboardingColors.line,
                            ),
                            padding: const EdgeInsets.symmetric(vertical: 15),
                            shape: const RoundedRectangleBorder(),
                          ),
                          child: const Text('已有账号，直接登录'),
                        ),
                      ),
                      const SizedBox(width: 9),
                      Expanded(
                        child: FilledButton(
                          onPressed: onRegister,
                          style: FilledButton.styleFrom(
                            backgroundColor: _OnboardingColors.cyan,
                            foregroundColor: _OnboardingColors.ink,
                            padding: const EdgeInsets.symmetric(vertical: 15),
                            shape: const RoundedRectangleBorder(),
                          ),
                          child: const Text('注册账号'),
                        ),
                      ),
                    ],
                  )
                : SizedBox(
                    key: ValueKey('next-$page'),
                    width: double.infinity,
                    child: FilledButton.icon(
                      onPressed: onNext,
                      icon: const Icon(Icons.arrow_forward_rounded, size: 18),
                      label: const Text('继续'),
                      style: FilledButton.styleFrom(
                        backgroundColor: _OnboardingColors.cyan,
                        foregroundColor: _OnboardingColors.ink,
                        padding: const EdgeInsets.symmetric(vertical: 15),
                        shape: const RoundedRectangleBorder(),
                      ),
                    ),
                  ),
          ),
        ),
      ),
    );
  }
}

class _FrameClock extends ChangeNotifier {
  static const _frameDuration = Duration(milliseconds: 84);

  Timer? _timer;
  int frame = 0;

  void start() {
    _timer = Timer.periodic(_frameDuration, (_) {
      frame++;
      notifyListeners();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }
}

class _BackdropGlyph {
  final double x;
  final int baseY;
  final int glyphIndex;

  const _BackdropGlyph({
    required this.x,
    required this.baseY,
    required this.glyphIndex,
  });
}

class _OnboardingBackdrop extends CustomPainter {
  static const _glyphs = '01{}[]<>/\\:=+#\$%&';

  final List<ui.Paragraph> _glyphParagraphs;
  final List<_BackdropGlyph> _backdropGlyphs;
  final Paint _gridPaint;

  _OnboardingBackdrop()
    : _glyphParagraphs = _buildGlyphParagraphs(),
      _backdropGlyphs = _buildBackdropGlyphs(),
      _gridPaint = Paint()
        ..color = _OnboardingColors.line.withValues(alpha: 0.16)
        ..strokeWidth = 1;

  static List<ui.Paragraph> _buildGlyphParagraphs() {
    return _glyphs.split('').map(_buildGlyphParagraph).toList();
  }

  static ui.Paragraph _buildGlyphParagraph(String glyph) {
    final builder =
        ui.ParagraphBuilder(
          ui.ParagraphStyle(fontFamily: 'monospace', fontSize: 10),
        )..pushStyle(
          ui.TextStyle(
            fontFamily: 'monospace',
            fontSize: 10,
            color: _OnboardingColors.cyan.withValues(alpha: 0.23),
          ),
        );
    builder.addText(glyph);
    final paragraph = builder.build();
    paragraph.layout(const ui.ParagraphConstraints(width: 12));
    return paragraph;
  }

  static List<_BackdropGlyph> _buildBackdropGlyphs() {
    return [
      for (var column = 0; column < 12; column++)
        for (var row = 0; row < 8; row++)
          _BackdropGlyph(
            x: column * 30.0 + (column * 17 % 5),
            baseY: row * 82 + column * 29,
            glyphIndex: (column * 17 + row * 7) % _glyphs.length,
          ),
    ];
  }

  @override
  void paint(Canvas canvas, Size size) {
    final width = size.width;
    final height = size.height;
    for (var x = 0.0; x < width; x += 34) {
      canvas.drawLine(Offset(x, 0), Offset(x, height), _gridPaint);
    }
    for (var y = 0.0; y < height; y += 34) {
      canvas.drawLine(Offset(0, y), Offset(width, y), _gridPaint);
    }

    for (final glyph in _backdropGlyphs) {
      if (glyph.x > width) continue;
      final y = (glyph.baseY % (height.toInt() + 40)) - 20;
      canvas.drawParagraph(
        _glyphParagraphs[glyph.glyphIndex],
        Offset(glyph.x, y.toDouble()),
      );
    }
  }

  @override
  bool shouldRepaint(covariant _OnboardingBackdrop oldDelegate) => false;
}
