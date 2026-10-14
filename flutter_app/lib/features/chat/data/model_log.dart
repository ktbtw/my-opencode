import 'package:flutter/foundation.dart';

/// 模型「恢复」与「列表」两条链路的流程日志。
///
/// - **恢复**：进入对话页时，把后端保存的模型与当前可用模型列表匹配，还原选中态。
/// - **列表**：打开模型选择面板，拉取可用模型、过滤、分组、选中并回写后端。
///
/// 统一使用 `[模型][恢复]` / `[模型][列表]` 前缀，便于在 logcat 中按链路过滤：
/// `adb logcat -s flutter:V | grep "\[模型\]"`
class ModelLog {
  const ModelLog._();

  static void step(String stage, String message) =>
      debugPrint('[模型][$stage] $message');

  // ------------------------------------------------------------------
  // 恢复链路
  // ------------------------------------------------------------------

  /// 页面触发了模型恢复（两个触发点之一）。
  static void restoreTriggered(String source, String agentId, int modelCount) =>
      step('恢复', '触发时机=$source，agent=$agentId，可用模型$modelCount个');

  /// 页面想恢复，但模型列表尚未就绪，等列表加载完成后再恢复。
  static void restorePending(String source) =>
      step('恢复', '触发时机=$source，模型列表尚未就绪，等待列表加载完成后恢复');

  /// 开始向后端查询已保存的模型。
  static void restoreStart(String agentId, int modelCount) =>
      step('恢复', '查询后端保存值：agent=$agentId，候选模型$modelCount个');

  /// 后端返回了保存值。
  static void restoreLoaded(String agentId, String saved) =>
      step('恢复', '后端保存值="${saved.isEmpty ? '（空）' : saved}"，agent=$agentId');

  /// 恢复成功，命中某个模型。
  static void restoreMatched(
    String agentId,
    String saved,
    String matched, {
    required String via,
  }) => step('恢复', '已还原为 "$matched"（匹配方式=$via，保存值="$saved"，agent=$agentId）');

  /// 保存值为空，回落到默认模型。
  static void restoreDefault(String agentId) =>
      step('恢复', '后端未保存模型，使用默认模型（agent=$agentId）');

  /// 保存值在可用列表中找不到对应模型。
  static void restoreNotFound(String agentId, String saved, int modelCount) =>
      step('恢复', '保存值 "$saved" 在可用列表中无匹配，保持未选中（候选$modelCount个，agent=$agentId）');

  /// 本次恢复结果被更新的操作取代，丢弃。
  static void restoreStale(String agentId, String saved) =>
      step('恢复', '结果已过期：期间发生了新的选择或恢复，丢弃本次结果（保存值="$saved"，agent=$agentId）');

  /// 跳过恢复。
  static void restoreSkipped(String agentId, int modelCount, String reason) =>
      step('恢复', '跳过：$reason（agent=$agentId，模型$modelCount个）');

  /// 恢复失败（网络异常等）。
  static void restoreFailed(String agentId, Object error) =>
      step('恢复', '失败：$error（agent=$agentId）');

  // ------------------------------------------------------------------
  // 列表链路
  // ------------------------------------------------------------------

  /// 发起可用模型请求。
  static void listRequested(String path) => step('列表', '请求可用模型 → $path');

  /// 可用模型加载完成。
  static void listLoaded(String path, int count, int elapsedMs) =>
      step('列表', '加载完成：$count个模型，耗时$elapsedMs毫秒（$path）');

  /// 可用模型加载失败。
  static void listFailed(String path, Object error, int elapsedMs) =>
      step('列表', '加载失败，耗时$elapsedMs毫秒：$error（$path）');

  /// 解析过滤统计，解释「设备明明有模型但列表为空」。
  static void listParsed({
    required int providers,
    required int connectedProviders,
    required int skippedProvider,
    required int skippedDeprecated,
    required int models,
  }) => step(
    '列表',
    '解析结果：供应商$providers个（已连接$connectedProviders个），'
        '因未连接跳过$skippedProvider个，因已废弃跳过$skippedDeprecated个，'
        '最终模型$models个',
  );

  /// 打开模型选择面板。
  static void pickerOpened(int modelCount, String? selected) =>
      step('列表', '打开选择面板：可选$modelCount个，当前选中=${selected ?? '默认模型'}');

  /// 面板展示统计（搜索后）。
  static void pickerShown(String search, int total, int filtered, int groups) =>
      step(
        '列表',
        '面板展示：搜索词="${search.isEmpty ? '（无）' : search}"，'
            '匹配$filtered/$total个，供应商$groups组',
      );

  /// 用户在面板中选中模型。
  static void pickerSelected(String? from, String? to) =>
      step('列表', '用户选中：${from ?? '默认模型'} → ${to ?? '默认模型（重置）'}');

  /// 选中结果已写入后端。
  static void selectSaved(
    String agentId,
    String? previous,
    String? next,
    int elapsedMs,
  ) => step(
    '列表',
    '选择已保存到后端：${previous ?? '默认模型'} → ${next ?? '默认模型'}，'
        '耗时$elapsedMs毫秒（agent=$agentId）',
  );

  /// 选中结果写入后端失败。
  static void selectSaveFailed(String agentId, Object error) =>
      step('列表', '选择保存到后端失败：$error（agent=$agentId）');
}
