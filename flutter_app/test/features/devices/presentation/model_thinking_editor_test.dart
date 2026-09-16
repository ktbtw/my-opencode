import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/devices/data/device_ai_config_model.dart';
import 'package:chat_codex_app/features/devices/presentation/widgets/model_thinking_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('applies context window preset without writing json', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(
      tester,
      model: DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
      }),
      onChanged: changes.add,
    );

    await tester.ensureVisible(
      find.byKey(const ValueKey('context-preset-1000000')),
    );
    await tester.tap(find.byKey(const ValueKey('context-preset-1000000')));
    await tester.pump();

    expect(changes.last.manualContextLimit, 1000000);
    expect(changes.last.contextLimit, 1000000);
    expect(changes.last.toJson()['context_limit'], 1000000);
  });

  testWidgets('applies output limit preset and can clear it', (tester) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(
      tester,
      model: DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
      }),
      onChanged: changes.add,
    );

    await tester.ensureVisible(
      find.byKey(const ValueKey('output-preset-16384')),
    );
    await tester.tap(find.byKey(const ValueKey('output-preset-16384')));
    await tester.pump();
    expect(changes.last.manualOutputLimit, 16384);
    expect(changes.last.outputLimit, 16384);

    await tester.ensureVisible(
      find.byKey(const ValueKey('output-preset-clear')),
    );
    await tester.tap(find.byKey(const ValueKey('output-preset-clear')));
    await tester.pump();
    expect(changes.last.manualOutputLimit, isNull);
    expect(changes.last.outputLimit, isNull);
  });

  testWidgets('rejects a non-positive context window', (tester) async {
    await _pumpEditor(
      tester,
      model: DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
      }),
      onChanged: (_) {},
    );

    await tester.enterText(
      find.byKey(const ValueKey('context-limit-input')),
      '0',
    );
    await tester.pump();

    expect(find.text('上下文窗口必须是正整数'), findsOneWidget);
  });

  testWidgets('shows inferred Kun context window in the editor header', (
    tester,
  ) async {
    await _pumpEditor(
      tester,
      model: DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
      }),
      onChanged: (_) {},
    );

    expect(find.text('Kun · 500k 窗口'), findsOneWidget);
  });

  testWidgets('shows detected capability and emits a manual disable override', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _detectedModel(), onChanged: changes.add);

    expect(find.text('模型接口声明该模型支持思考控制'), findsOneWidget);
    expect(find.text('低'), findsOneWidget);
    expect(find.text('高'), findsOneWidget);

    await tester.tap(find.text('手动覆盖'));
    await tester.pumpAndSettle();
    expect(changes.last.thinking?.overrideEnabled, isTrue);

    await tester.tap(find.byType(Switch));
    await tester.pump();
    expect(changes.last.thinking?.supported, isFalse);
    expect(changes.last.thinking?.overrideEnabled, isTrue);
    expect(changes.last.variants, isEmpty);
  });

  testWidgets('rejects a non-positive token budget', (tester) async {
    await _pumpEditor(tester, model: _budgetModel(), onChanged: (_) {});

    await tester.enterText(_valueFields().first, '0');
    await tester.ensureVisible(find.text('校验配置'));
    await tester.tap(find.text('校验配置'));
    await tester.pump();

    expect(find.text('Token 预算必须是正整数'), findsOneWidget);
  });

  testWidgets('reports an invalid advanced variants shape', (tester) async {
    await _pumpEditor(tester, model: _budgetModel(), onChanged: (_) {});

    await tester.ensureVisible(find.text('高级参数'));
    await tester.tap(find.text('高级参数'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const ValueKey('thinking-variants-json')),
      '[]',
    );
    await tester.ensureVisible(find.text('格式化并应用'));
    await tester.tap(find.text('格式化并应用'));
    await tester.pump();

    expect(find.text('variants 必须是 JSON 对象'), findsOneWidget);
  });

  testWidgets('reverse parses snake case reasoning effort values', (
    tester,
  ) async {
    await _pumpEditor(tester, model: _budgetModel(), onChanged: (_) {});

    await tester.ensureVisible(find.text('高级参数'));
    await tester.tap(find.text('高级参数'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const ValueKey('thinking-variants-json')),
      '{"省流":{"reasoning_effort":"low"},"深度":{"reasoning_effort":"high"}}',
    );
    await tester.ensureVisible(find.text('格式化并应用'));
    await tester.tap(find.text('格式化并应用'));
    await tester.pump();

    final valueFields = _valueFields();
    expect(valueFields, findsNWidgets(2));
    expect(
      tester.widget<TextFormField>(valueFields.at(0)).controller?.text,
      'low',
    );
    expect(
      tester.widget<TextFormField>(valueFields.at(1)).controller?.text,
      'high',
    );
  });

  testWidgets('edits input modalities with text always enabled', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _detectedModel(), onChanged: changes.add);

    final textChip = tester.widget<ChoiceChip>(
      find.ancestor(of: find.text('文本'), matching: find.byType(ChoiceChip)),
    );
    expect(textChip.onSelected, isNull);
    expect(textChip.selected, isTrue);

    await tester.tap(
      find.ancestor(of: find.text('图片'), matching: find.byType(ChoiceChip)),
    );
    await tester.pump();

    expect(changes.last.inputModalities, ['text', 'image']);
    expect(changes.last.outputModalities, ['text']);
  });

  testWidgets('reflects existing input modalities and toggles off', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(
      tester,
      model: const DeviceAIModelInfo(
        id: 'kimi-k3',
        name: 'kimi-k3',
        ownedBy: 'kimi',
        inputModalities: ['text', 'image', 'video'],
        outputModalities: ['text'],
      ),
      onChanged: changes.add,
    );

    final videoChip = tester.widget<ChoiceChip>(
      find.ancestor(of: find.text('视频'), matching: find.byType(ChoiceChip)),
    );
    expect(videoChip.selected, isTrue);

    await tester.tap(
      find.ancestor(of: find.text('视频'), matching: find.byType(ChoiceChip)),
    );
    await tester.pump();

    expect(changes.last.inputModalities, ['text', 'image']);
    expect(changes.last.outputModalities, ['text']);
  });

  testWidgets('manual editor fits a 320px mobile viewport', (tester) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: Scaffold(
          body: ModelThinkingEditor(
            model: _budgetModel(),
            onChanged: (_) {},
            onBack: () {},
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Claude Test'), findsOneWidget);
    expect(find.text('Token 预算'), findsWidgets);
    expect(tester.takeException(), isNull);
  });

  testWidgets('toggles a mainstream effort preset on then off', (tester) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _detectedModel(), onChanged: changes.add);

    await tester.tap(find.text('手动覆盖'));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('thinking-preset-low')));
    await tester.pump();
    expect(changes.last.variants.keys.toList(), ['high']);
    expect(_valueFields(), findsOneWidget);
    expect(
      tester.widget<TextFormField>(_valueFields()).controller?.text,
      'high',
    );

    await tester.ensureVisible(
      find.byKey(const ValueKey('thinking-preset-xhigh')),
    );
    await tester.tap(find.byKey(const ValueKey('thinking-preset-xhigh')));
    await tester.pump();
    expect(changes.last.variants.keys.toList(), ['high', 'xhigh']);
    expect(changes.last.variants['xhigh'], {
      'reasoning': {'effort': 'xhigh'},
    });

    await tester.tap(find.byKey(const ValueKey('thinking-preset-xhigh')));
    await tester.pump();
    expect(changes.last.variants.keys.toList(), ['high']);
  });

  testWidgets('deleting the middle level keeps the surrounding rows', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _detectedModel(), onChanged: changes.add);

    await tester.tap(find.text('手动覆盖'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(
      find.byKey(const ValueKey('thinking-preset-add-all')),
    );
    await tester.tap(find.byKey(const ValueKey('thinking-preset-add-all')));
    await tester.pump();

    expect(_fieldTexts(tester, _nameFields()), [
      'low',
      'high',
      'none',
      'minimal',
      'medium',
      'xhigh',
      'max',
    ]);

    await tester.tap(find.byIcon(Icons.delete_outline_rounded).at(1));
    await tester.pump();

    expect(_fieldTexts(tester, _nameFields()), [
      'low',
      'none',
      'minimal',
      'medium',
      'xhigh',
      'max',
    ]);
    expect(changes.last.variants.keys.toList(), [
      'low',
      'none',
      'minimal',
      'medium',
      'xhigh',
      'max',
    ]);
  });

  testWidgets('adds all mainstream effort presets in one tap', (tester) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _detectedModel(), onChanged: changes.add);

    await tester.tap(find.text('手动覆盖'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(
      find.byKey(const ValueKey('thinking-preset-add-all')),
    );
    await tester.tap(find.byKey(const ValueKey('thinking-preset-add-all')));
    await tester.pump();

    expect(changes.last.variants.keys.toList(), [
      'low',
      'high',
      'none',
      'minimal',
      'medium',
      'xhigh',
      'max',
    ]);
    expect(changes.last.variants['none'], {
      'reasoning': {'effort': 'none'},
    });
    expect(changes.last.variants['max'], {
      'reasoning': {'effort': 'max'},
    });
  });

  testWidgets('adds remaining budget presets from the mainstream chips', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _budgetModel(), onChanged: changes.add);

    expect(find.text('低 4096'), findsOneWidget);
    expect(find.text('最大 32000'), findsOneWidget);

    await tester.ensureVisible(
      find.byKey(const ValueKey('thinking-preset-add-all')),
    );
    await tester.tap(find.byKey(const ValueKey('thinking-preset-add-all')));
    await tester.pump();

    expect(changes.last.variants.keys.toList(), [
      'high',
      'low',
      'medium',
      'max',
    ]);
    expect(changes.last.variants['low'], {
      'thinking': {'type': 'enabled', 'budgetTokens': 4096},
    });
    expect(changes.last.variants['max'], {
      'thinking': {'type': 'enabled', 'budgetTokens': 32000},
    });
  });

  testWidgets('toggle control keeps the nested reasoning protocol shape', (
    tester,
  ) async {
    final changes = <DeviceAIModelInfo>[];
    await _pumpEditor(tester, model: _toggleModel(), onChanged: changes.add);

    expect(find.text('通用嵌套 · reasoning.effort'), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pump();
    await tester.tap(find.byType(Switch));
    await tester.pump();

    expect(changes.last.variants, {
      'enabled': {
        'reasoning': {'enabled': true},
      },
    });
  });
}

Finder _nameFields() {
  return find.byWidgetPredicate(
    (widget) =>
        widget is TextFormField &&
        widget.key is ValueKey<String> &&
        (widget.key as ValueKey<String>).value.startsWith('thinking-name-'),
  );
}

Finder _valueFields() {
  return find.byWidgetPredicate(
    (widget) =>
        widget is TextFormField &&
        widget.key is ValueKey<String> &&
        (widget.key as ValueKey<String>).value.startsWith('thinking-value-'),
  );
}

List<String> _fieldTexts(WidgetTester tester, Finder finder) {
  return tester
      .widgetList<TextFormField>(finder)
      .map((field) => field.controller?.text ?? field.initialValue ?? '')
      .toList();
}

Future<void> _pumpEditor(
  WidgetTester tester, {
  required DeviceAIModelInfo model,
  required ValueChanged<DeviceAIModelInfo> onChanged,
}) async {
  tester.view.physicalSize = const Size(800, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      theme: AppTheme.light,
      home: Scaffold(
        body: ModelThinkingEditor(model: model, onChanged: onChanged),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

DeviceAIModelInfo _detectedModel() {
  const variants = <String, dynamic>{
    'low': {
      'reasoning': {'effort': 'low'},
    },
    'high': {
      'reasoning': {'effort': 'high'},
    },
  };
  return const DeviceAIModelInfo(
    id: 'stealth/ox-alpha',
    name: 'Ox Alpha',
    ownedBy: 'openrouter',
    variants: variants,
    thinking: DeviceAIThinkingInfo(
      supported: true,
      source: 'provider',
      control: 'effort',
      protocol: 'openrouter',
      supportedParameters: ['reasoning'],
      variants: variants,
    ),
  );
}

DeviceAIModelInfo _budgetModel() {
  const variants = <String, dynamic>{
    'high': {
      'thinking': {'type': 'enabled', 'budgetTokens': 16000},
    },
  };
  return const DeviceAIModelInfo(
    id: 'claude-test',
    name: 'Claude Test',
    ownedBy: 'anthropic',
    variants: variants,
    thinking: DeviceAIThinkingInfo(
      supported: true,
      source: 'manual',
      control: 'budget',
      protocol: 'anthropic',
      overrideEnabled: true,
      variants: variants,
      overrideVariants: variants,
    ),
  );
}

DeviceAIModelInfo _toggleModel() {
  const variants = <String, dynamic>{
    'enabled': {
      'reasoning': {'enabled': true},
    },
  };
  return const DeviceAIModelInfo(
    id: 'reasoning-toggle-model',
    name: 'Reasoning Toggle',
    ownedBy: 'custom',
    variants: variants,
    thinking: DeviceAIThinkingInfo(
      supported: true,
      source: 'manual',
      control: 'toggle',
      protocol: 'reasoning',
      overrideEnabled: true,
      variants: variants,
      overrideVariants: variants,
    ),
  );
}
