import '../../../core/storage/app_storage.dart';

/// 上传策略：由服务端按会员等级下发，客户端不自行猜测。
/// 普通用户为 512KB 分块、单并发；会员为 5MB 分块、多并发。
class UploadPolicy {
  final String tier;
  final bool isMember;
  final int chunkSize;
  final int maxChunkSize;
  final int concurrency;
  final int targetBytesPerSecond;
  final bool resumeEnabled;

  const UploadPolicy({
    required this.tier,
    required this.isMember,
    required this.chunkSize,
    required this.maxChunkSize,
    required this.concurrency,
    required this.targetBytesPerSecond,
    required this.resumeEnabled,
  });

  /// 普通用户默认策略，与服务端 free 等级保持一致。
  static const UploadPolicy free = UploadPolicy(
    tier: 'free',
    isMember: false,
    chunkSize: 512 * 1024,
    maxChunkSize: 512 * 1024,
    concurrency: 1,
    targetBytesPerSecond: 512 * 1024,
    resumeEnabled: true,
  );

  /// 会员默认策略，与服务端 plus 等级保持一致。
  static const UploadPolicy member = UploadPolicy(
    tier: 'plus',
    isMember: true,
    chunkSize: 5 * 1024 * 1024,
    maxChunkSize: 5 * 1024 * 1024,
    concurrency: 3,
    targetBytesPerSecond: 5 * 1024 * 1024,
    resumeEnabled: true,
  );

  factory UploadPolicy.fromJson(Map<String, dynamic> json) {
    final tier = (json['tier'] as String?)?.trim().toLowerCase() ?? 'free';
    final isMember =
        json['is_member'] as bool? ?? (tier == 'plus' || tier == 'pro');
    final chunkSize = _positiveInt(json['chunk_size'], UploadPolicy.free.chunkSize);
    return UploadPolicy(
      tier: tier,
      isMember: isMember,
      chunkSize: chunkSize,
      maxChunkSize: _positiveInt(json['max_chunk_size'], chunkSize),
      concurrency: _positiveInt(
        json['concurrency'],
        isMember ? UploadPolicy.member.concurrency : 1,
      ),
      targetBytesPerSecond: _positiveInt(
        json['target_bytes_per_second'],
        isMember ? UploadPolicy.member.targetBytesPerSecond : UploadPolicy.free.targetBytesPerSecond,
      ),
      resumeEnabled: json['resume_enabled'] as bool? ?? true,
    );
  }

  static int _positiveInt(Object? value, int fallback) {
    if (value is int && value > 0) return value;
    if (value is num && value > 0) return value.toInt();
    return fallback;
  }

  /// 按本地缓存的会员等级推断策略，用于服务端策略尚未拉取时兜底。
  static UploadPolicy fromLocalMembership() {
    return AppStorage.isMember ? UploadPolicy.member : UploadPolicy.free;
  }
}
