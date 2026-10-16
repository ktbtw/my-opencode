import 'package:flutter/material.dart';

import '../../../core/config/api_client.dart';
import '../../../core/theme/app_colors.dart';

/// 我的额度：展示会员等级、网关记账的已用/剩余额度、可用模型与设备下发状态。
///
/// 数据来自 `GET /api/membership/usage`（平台侧读网关的钥匙记账）。
class MembershipUsagePage extends StatefulWidget {
  const MembershipUsagePage({super.key});

  @override
  State<MembershipUsagePage> createState() => _MembershipUsagePageState();
}

class _MembershipUsagePageState extends State<MembershipUsagePage> {
  bool _loading = true;
  String? _error;
  Map<String, dynamic>? _data;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final data = await ApiClient.get('/api/membership/usage');
      if (!mounted) return;
      setState(() {
        _data = data;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error.toString();
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        title: const Text('我的额度'),
        backgroundColor: AppColors.surface,
        foregroundColor: AppColors.textPrimary,
        elevation: 0,
        actions: [
          IconButton(
            onPressed: _loading ? null : _load,
            icon: const Icon(Icons.refresh),
            tooltip: '刷新',
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: _buildBody(),
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return ListView(
        padding: const EdgeInsets.all(20),
        children: [
          _card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('加载失败', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                const SizedBox(height: 8),
                Text(_error!, style: const TextStyle(color: AppColors.textSecondary, fontSize: 13)),
                const SizedBox(height: 12),
                TextButton(onPressed: _load, child: const Text('重试')),
              ],
            ),
          ),
        ],
      );
    }
    final data = _data ?? const {};
    final tier = (data['tier'] ?? 'free').toString();
    final isMember = data['is_member'] == true;
    final active = data['active'] == true;
    final groupName = (data['group_name'] ?? '').toString();
    final limits = (data['limits'] as Map?)?.cast<String, dynamic>() ?? const {};
    final used = (data['used'] as Map?)?.cast<String, dynamic>() ?? const {};
    final models = (data['models'] as List?) ?? const [];
    final devices = (data['device_status'] as Map?)?.cast<String, dynamic>() ?? const {};

    final totalMicro = _num(limits['max_cost_micro']);
    final usedMicro = _num(used['cost_micro']);
    final dailyMicro = _num(limits['daily_cost_micro']);
    final dayUsedMicro = _num(used['day_cost_micro']);
    final dailyTokens = _num(limits['daily_tokens']);
    final dayTokens = _num(used['day_tokens']);

    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        _card(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  _tierBadge(tier),
                  const SizedBox(width: 10),
                  if (groupName.isNotEmpty)
                    Expanded(
                      child: Text(groupName, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                    ),
                  _statusPill(
                    active ? '已开通' : (isMember ? '待开通' : '未开通'),
                    active ? AppColors.statusSuccess : AppColors.textMuted,
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Text(
                active
                    ? '额度由网关按调用实时记账，超限后请求会被自动拒绝。'
                    : '当前等级还没有专属模型额度。',
                style: const TextStyle(color: AppColors.textSecondary, fontSize: 13, height: 1.5),
              ),
            ],
          ),
        ),
        if (active && totalMicro > 0) ...[
          const SizedBox(height: 12),
          _card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _quotaRow('总额度', usedMicro, totalMicro, '已用', '剩余'),
                const SizedBox(height: 16),
                if (dailyMicro > 0) _quotaRow('今日额度', dayUsedMicro, dailyMicro, '今日已用', '今日剩余'),
                if (dailyTokens > 0) ...[
                  const SizedBox(height: 16),
                  // token 是原始计数，不按金额（1e-6）换算
                  _quotaRow('今日 Token', dayTokens, dailyTokens, '今日已用', '今日剩余', count: true),
                ],
                const SizedBox(height: 12),
                Text(
                  '额度按 UTC 日重置${data['reset_at'] != null ? '，下次重置：${_localTime(data['reset_at'].toString())}' : ''}',
                  style: const TextStyle(color: AppColors.textMuted, fontSize: 12),
                ),
              ],
            ),
          ),
        ],
        const SizedBox(height: 12),
        _card(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('可用模型', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
              const SizedBox(height: 10),
              if (models.isEmpty)
                const Text('暂无专属模型', style: TextStyle(color: AppColors.textMuted, fontSize: 13))
              else
                ...models.map((item) {
                  final map = (item as Map).cast<String, dynamic>();
                  final id = (map['id'] ?? '').toString();
                  final name = (map['display_name'] ?? '').toString();
                  final real = (map['real_model'] ?? '').toString();
                  return Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(name.isEmpty ? id : name, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
                        const SizedBox(height: 2),
                        Text('模型 ID：$id', style: const TextStyle(fontSize: 12, color: AppColors.textSecondary)),
                        if (real.isNotEmpty)
                          Text('底层模型：$real', style: const TextStyle(fontSize: 12, color: AppColors.textMuted)),
                      ],
                    ),
                  );
                }),
            ],
          ),
        ),
        if (devices.isNotEmpty) ...[
          const SizedBox(height: 12),
          _card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('设备下发状态', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                const SizedBox(height: 8),
                ...devices.entries.map(
                  (entry) => Padding(
                    padding: const EdgeInsets.only(bottom: 6),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            _shortMachineId(entry.key),
                            style: const TextStyle(fontSize: 13, color: AppColors.textSecondary),
                          ),
                        ),
                        _statusPill(
                          entry.value.toString() == 'ok' ? '已生效' : entry.value.toString(),
                          entry.value.toString() == 'ok' ? AppColors.statusSuccess : AppColors.statusWarning,
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
        const SizedBox(height: 16),
        const Text(
          '额度用尽或等级到期后，专属模型会自动从设备上撤下；升级/续费后几秒内自动恢复。',
          style: TextStyle(color: AppColors.textMuted, fontSize: 12, height: 1.5),
        ),
      ],
    );
  }

  Widget _quotaRow(String title, double usedValue, double totalValue, String usedLabel, String remainLabel, {String unit = '\$', bool count = false}) {
    final remaining = (totalValue - usedValue).clamp(0, double.infinity).toDouble();
    final ratio = totalValue > 0 ? (usedValue / totalValue).clamp(0.0, 1.0) : 0.0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(title, style: const TextStyle(fontSize: 13, color: AppColors.textSecondary)),
            const Spacer(),
            Text(
              '$usedLabel ${_amount(usedValue, unit, count: count)} / ${_amount(totalValue, unit, count: count)}',
              style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
          ],
        ),
        const SizedBox(height: 6),
        ClipRRect(
          borderRadius: BorderRadius.circular(6),
          child: LinearProgressIndicator(
            value: ratio,
            minHeight: 8,
            backgroundColor: AppColors.borderLight,
            valueColor: AlwaysStoppedAnimation<Color>(
              ratio >= 0.9 ? AppColors.statusWarning : AppColors.primary,
            ),
          ),
        ),
        const SizedBox(height: 4),
        Text(
          '$remainLabel ${_amount(remaining, unit, count: count)}',
          style: const TextStyle(fontSize: 12, color: AppColors.textSecondary),
        ),
      ],
    );
  }

  Widget _tierBadge(String tier) {
    final label = tier == 'pro'
        ? 'Pro 会员'
        : tier == 'plus'
            ? 'Plus 会员'
            : '普通用户';
    final color = tier == 'free' ? AppColors.textMuted : AppColors.primary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(label, style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700, color: color)),
    );
  }

  Widget _statusPill(String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(text, style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: color)),
    );
  }

  Widget _card({required Widget child}) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: AppColors.borderLight),
      ),
      child: child,
    );
  }
}

double _num(Object? value) {
  if (value is num) return value.toDouble();
  return double.tryParse(value?.toString() ?? '') ?? 0;
}

String _amount(double raw, String unit, {bool count = false}) {
  if (count) {
    // 原始计数（token）：加千分位，不做 1e-6 缩放
    final digits = raw.round().toString();
    final buffer = StringBuffer();
    for (var i = 0; i < digits.length; i++) {
      if (i > 0 && (digits.length - i) % 3 == 0) buffer.write(',');
      buffer.write(digits[i]);
    }
    return buffer.toString();
  }
  final value = raw / 1e6;
  final text = value >= 1 ? value.toStringAsFixed(2) : value.toStringAsFixed(4);
  return '$unit$text';
}

String _localTime(String iso) {
  final parsed = DateTime.tryParse(iso);
  if (parsed == null) return iso;
  final local = parsed.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)}';
}

String _shortMachineId(String id) {
  if (id.length <= 14) return id;
  return '${id.substring(0, 8)}…${id.substring(id.length - 4)}';
}
