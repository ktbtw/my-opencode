import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

class AppSettings {
  // 展示类
  final bool markdownRender;
  final bool latexRender;
  final bool mermaidRender;
  final bool htmlPreview;

  // 统计类
  final bool showFirstTokenTime;
  final bool showTokenCount;
  final bool showWordCount;

  // 输入类
  final bool pasteAsFile;
  static const int pasteAsFileThreshold = 500; // 超过多少字符视为长文本

  const AppSettings({
    this.markdownRender = true,
    this.latexRender = true,
    this.mermaidRender = true,
    this.htmlPreview = true,
    this.showFirstTokenTime = false,
    this.showTokenCount = false,
    this.showWordCount = false,
    this.pasteAsFile = false,
  });

  AppSettings copyWith({
    bool? markdownRender,
    bool? latexRender,
    bool? mermaidRender,
    bool? htmlPreview,
    bool? showFirstTokenTime,
    bool? showTokenCount,
    bool? showWordCount,
    bool? pasteAsFile,
  }) {
    return AppSettings(
      markdownRender: markdownRender ?? this.markdownRender,
      latexRender: latexRender ?? this.latexRender,
      mermaidRender: mermaidRender ?? this.mermaidRender,
      htmlPreview: htmlPreview ?? this.htmlPreview,
      showFirstTokenTime: showFirstTokenTime ?? this.showFirstTokenTime,
      showTokenCount: showTokenCount ?? this.showTokenCount,
      showWordCount: showWordCount ?? this.showWordCount,
      pasteAsFile: pasteAsFile ?? this.pasteAsFile,
    );
  }
}

class SettingsNotifier extends StateNotifier<AppSettings> {
  SettingsNotifier() : super(const AppSettings()) {
    _load();
  }

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    state = AppSettings(
      markdownRender: prefs.getBool('markdownRender') ?? true,
      latexRender: prefs.getBool('latexRender') ?? true,
      mermaidRender: prefs.getBool('mermaidRender') ?? true,
      htmlPreview: prefs.getBool('htmlPreview') ?? true,
      showFirstTokenTime: prefs.getBool('showFirstTokenTime') ?? false,
      showTokenCount: prefs.getBool('showTokenCount') ?? false,
      showWordCount: prefs.getBool('showWordCount') ?? false,
      pasteAsFile: prefs.getBool('pasteAsFile') ?? false,
    );
  }

  Future<void> toggle(String key) async {
    final prefs = await SharedPreferences.getInstance();
    final current = _get(key);
    final next = !current;
    await prefs.setBool(key, next);
    state = _set(key, next);
  }

  bool _get(String key) => switch (key) {
        'markdownRender' => state.markdownRender,
        'latexRender' => state.latexRender,
        'mermaidRender' => state.mermaidRender,
        'htmlPreview' => state.htmlPreview,
        'showFirstTokenTime' => state.showFirstTokenTime,
        'showTokenCount' => state.showTokenCount,
        'showWordCount' => state.showWordCount,
        'pasteAsFile' => state.pasteAsFile,
        _ => false,
      };

  AppSettings _set(String key, bool value) => switch (key) {
        'markdownRender' => state.copyWith(markdownRender: value),
        'latexRender' => state.copyWith(latexRender: value),
        'mermaidRender' => state.copyWith(mermaidRender: value),
        'htmlPreview' => state.copyWith(htmlPreview: value),
        'showFirstTokenTime' => state.copyWith(showFirstTokenTime: value),
        'showTokenCount' => state.copyWith(showTokenCount: value),
        'showWordCount' => state.copyWith(showWordCount: value),
        'pasteAsFile' => state.copyWith(pasteAsFile: value),
        _ => state,
      };
}

final settingsProvider =
    StateNotifierProvider<SettingsNotifier, AppSettings>(
  (ref) => SettingsNotifier(),
);
