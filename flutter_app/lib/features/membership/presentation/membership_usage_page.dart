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
    final devices = (data['devices'] as List?) ?? const [];

    final totalMicro = _num(limits['max_cost_micro']);
    final usedMicro = _num(used['cost_micro']);
    final dailyMicro = _num(limits['daily_cost_micro']);
    final dayUsedMicro = _num(used['day_cost_micro']);
    final dailyTokens = _num(limits['daily_tokens']);
    final dayTokens = _num(used['day_tokens']);
    // 剩余值优先用后端算好的（避免四舍五入差异），没给就本地兜底
    final remainingMicro = _num(data['remaining_cost_micro']) > 0 || totalMicro > 0
        ? _num(data['remaining_cost_micro'])
        : (totalMicro - usedMicro).clamp(0, double.infinity).toDouble();
    final remainingDailyMicro = data['remaining_daily_cost_micro'] != null
        ? _num(data['remaining_daily_cost_micro'])
        : (dailyMicro - dayUsedMicro).clamp(0, double.infinity).toDouble();
    final remainingDailyTokens = data['remaining_daily_tokens'] != null
        ? _num(data['remaining_daily_tokens'])
        : (dailyTokens - dayTokens).clamp(0, double.infinity).toDouble();
    // 任意一种额度存在就展示这张卡（之前漏了"只有日费用"的情况）
    final hasQuota =
        dailyTokens > 0 || dailyMicro > 0 || totalMicro > 0;

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
                    ? '专属模型在你当前额度内可正常使用；额度用完会暂停，恢复后自动可用。'
                    : '当前等级还没有专属模型。',
                style: const TextStyle(color: AppColors.textSecondary, fontSize: 13, height: 1.5),
              ),
            ],
          ),
        ),
        if (active && hasQuota) ...[
          const SizedBox(height: 12),
          _card(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // 大字余额：有总余额就显示余额，否则显示今日剩余
                if (totalMicro > 0) ...[
                  _headline('余额', _amount(remainingMicro, '\$')),
                  const SizedBox(height: 8),
                  _quotaRow('总额度', usedMicro, totalMicro, '已用', '剩余'),
                ] else if (dailyMicro > 0) ...[
                  _headline('今日剩余', _amount(remainingDailyMicro, '\$')),
                  const SizedBox(height: 8),
                  _quotaRow('今日额度', dayUsedMicro, dailyMicro, '已用', '剩余'),
                ] else if (dailyTokens > 0) ...[
                  _headline('今日剩余', _amount(remainingDailyTokens, '', count: true)),
                  const SizedBox(height: 8),
                  _quotaRow('今日额度', dayTokens, dailyTokens, '已用', '剩余', count: true),
                ],
                const SizedBox(height: 12),
                if (dailyMicro > 0 && totalMicro > 0)
                  _costLine('今日费用', dayUsedMicro, dailyMicro),
                if (dailyTokens > 0 && (dailyMicro > 0 || totalMicro > 0))
                  _costLine('今日 Token 用量', dayTokens, dailyTokens, count: true),
                const SizedBox(height: 4),
                Text(
                  _resetText(data['reset_at']),
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
              const Text('专属模型', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
              const SizedBox(height: 10),
              if (models.isEmpty)
                const Text('暂未开通专属模型', style: TextStyle(color: AppColors.textMuted, fontSize: 13))
              else
                ...models.map((item) {
                  final map = (item as Map).cast<String, dynamic>();
                  final name = (map['name'] ?? '').toString();
                  return Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: Row(
                      children: [
                        const Icon(Icons.check_circle_outline, size: 16, color: AppColors.statusSuccess),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            name,
                            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
                          ),
                        ),
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
                const Text('可用设备', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                const SizedBox(height: 8),
                ...devices.map(
                  (item) {
                    final map = (item as Map).cast<String, dynamic>();
                    final name = (map['name'] ?? '').toString();
                    final ready = map['ready'] == true;
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 6),
                      child: Row(
                        children: [
                          Expanded(
                            child: Text(
                              name,
                              style: const TextStyle(fontSize: 14, color: AppColors.textPrimary),
                            ),
                          ),
                          _statusPill(
                            ready ? '已就绪' : '待同步',
                            ready ? AppColors.statusSuccess : AppColors.statusWarning,
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ],
            ),
          ),
        ],
        const SizedBox(height: 16),
        const Text(
          '额度用完或会员到期后，专属模型会暂停使用；恢复额度或续费后自动可用。',
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

  /// 大字余额/剩余：一眼能看到还能用多少
  Widget _headline(String title, String value) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.baseline,
      textBaseline: TextBaseline.alphabetic,
      children: [
        Text(title, style: const TextStyle(fontSize: 13, color: AppColors.textSecondary)),
        const SizedBox(width: 8),
        Text(
          value,
          style: const TextStyle(
            fontSize: 26,
            fontWeight: FontWeight.w700,
            color: AppColors.textPrimary,
          ),
        ),
      ],
    );
  }

  /// 费用明细：只报数，不画进度条（额度以 token 为准，费用是成本口径）
  Widget _costLine(String title, double usedValue, double limitValue, {bool count = false}) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        children: [
          Text(title, style: const TextStyle(fontSize: 13, color: AppColors.textSecondary)),
          const Spacer(),
          Text(
            count
                ? '${_amount(usedValue, '', count: true)} / ${_amount(limitValue, '', count: true)}'
                : '${_amount(usedValue, '\$')} / ${_amount(limitValue, '\$')}',
            style: const TextStyle(fontSize: 13, color: AppColors.textSecondary),
          ),
        ],
      ),
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
    // token 用 亿/万 展示，短数字才用千分位
    if (raw >= 1e8) return '${_trimNumber(raw / 1e8)} 亿';
    if (raw >= 1e4) return '${_trimNumber(raw / 1e4)} 万';
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

/// 说人话的重置说明：直接给出本地时间点，不暴露内部时区概念。
String _trimNumber(double value) {
  if (value == value.roundToDouble()) return value.toStringAsFixed(0);
  return value.toStringAsFixed(2);
}

String _resetText(Object? resetAt) {
  final text = _localTime(resetAt?.toString() ?? '');
  final clock = text.contains(' ') ? text.split(' ').last : text;
  return '今日额度每天 $clock 重置';
}

String _localTime(String iso) {
  final parsed = DateTime.tryParse(iso);
  if (parsed == null) return iso;
  final local = parsed.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)}';
}
