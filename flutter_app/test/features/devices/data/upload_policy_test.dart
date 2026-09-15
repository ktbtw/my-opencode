import 'package:chat_codex_app/features/devices/data/upload_policy.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('UploadPolicy', () {
    test('free 等级为 512KB 单并发', () {
      const policy = UploadPolicy.free;
      expect(policy.isMember, isFalse);
      expect(policy.chunkSize, 512 * 1024);
      expect(policy.concurrency, 1);
    });

    test('member 等级为 5MB 多并发，目标约 5MB/s', () {
      const policy = UploadPolicy.member;
      expect(policy.isMember, isTrue);
      expect(policy.chunkSize, 5 * 1024 * 1024);
      expect(policy.concurrency, greaterThan(1));
      expect(policy.targetBytesPerSecond, 5 * 1024 * 1024);
    });

    test('解析服务端下发的会员策略', () {
      final policy = UploadPolicy.fromJson({
        'tier': 'plus',
        'is_member': true,
        'chunk_size': 5 * 1024 * 1024,
        'max_chunk_size': 5 * 1024 * 1024,
        'concurrency': 3,
        'target_bytes_per_second': 5 * 1024 * 1024,
        'resume_enabled': true,
      });
      expect(policy.tier, 'plus');
      expect(policy.isMember, isTrue);
      expect(policy.chunkSize, 5 * 1024 * 1024);
      expect(policy.concurrency, 3);
      expect(policy.resumeEnabled, isTrue);
    });

    test('解析服务端下发的普通用户策略', () {
      final policy = UploadPolicy.fromJson({
        'tier': 'free',
        'is_member': false,
        'chunk_size': 512 * 1024,
        'max_chunk_size': 512 * 1024,
        'concurrency': 1,
        'target_bytes_per_second': 512 * 1024,
      });
      expect(policy.isMember, isFalse);
      expect(policy.chunkSize, 512 * 1024);
      expect(policy.concurrency, 1);
    });

    test('缺失字段时回退到安全默认值', () {
      final policy = UploadPolicy.fromJson({'tier': 'free'});
      expect(policy.chunkSize, 512 * 1024);
      expect(policy.concurrency, 1);
      expect(policy.isMember, isFalse);
      // resume_enabled 未提供时默认开启，保证断点续传可用。
      expect(policy.resumeEnabled, isTrue);
    });

    test('非法数值不会被采纳为分块大小', () {
      final policy = UploadPolicy.fromJson({
        'tier': 'plus',
        'is_member': true,
        'chunk_size': 0,
        'concurrency': -3,
      });
      expect(policy.chunkSize, UploadPolicy.free.chunkSize);
      expect(policy.concurrency, greaterThan(0));
    });
  });
}
