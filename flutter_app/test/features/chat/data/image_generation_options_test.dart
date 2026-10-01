import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('生图参数只走 metadata，不拼进提示词', () {
    const options = ImageGenerationOptions(
      size: '1024x1536',
      count: 4,
      quality: 'high',
      format: 'png',
    );

    expect(options.toMetadata(), {
      'image_size': '1024x1536',
      'image_count': '4',
      'image_quality': 'high',
      'image_format': 'png',
    });
  });

  test('视频参数同样走 metadata', () {
    const options = ImageGenerationOptions(
      duration: 8,
      aspectRatio: '16:9',
      resolution: '720p',
    );

    expect(options.toMetadata(), {
      'image_duration': '8',
      'image_aspect_ratio': '16:9',
      'image_resolution': '720p',
    });
  });

  test('未设置参数时不产生任何字段', () {
    const options = ImageGenerationOptions();
    expect(options.isEmpty, isTrue);
    expect(options.toMetadata(), isEmpty);
  });

  test('张数按 opencode 的上限收敛', () {
    const tooMany = ImageGenerationOptions(count: 99);
    expect(
      tooMany.toMetadata()['image_count'],
      ImageGenerationOptions.maxCount.toString(),
    );
  });

  test('metadata 只包含已设置的字段', () {
    const options = ImageGenerationOptions(size: '1024x1024', count: 2);
    expect(options.toMetadata(), {
      'image_size': '1024x1024',
      'image_count': '2',
    });

    const xai = ImageGenerationOptions(
      aspectRatio: '16:9',
      resolution: '2k',
    );
    expect(xai.toMetadata(), {
      'image_aspect_ratio': '16:9',
      'image_resolution': '2k',
    });
  });

  test('copyWith 可以单独清除某一项', () {
    const options = ImageGenerationOptions(
      size: '1024x1024',
      count: 4,
      quality: 'high',
    );

    final cleared = options.copyWith(clearCount: true);

    expect(cleared.count, isNull);
    expect(cleared.size, '1024x1024');
    expect(cleared.quality, 'high');
  });

  test('image capabilities 解析上报的参数集合', () {
    final caps = ImageGenerationCapabilities.fromJson({
      'sizes': const ['1024x1024', '1536x1024'],
      'aspect_ratios': const ['1:1', '16:9'],
      'resolutions': const ['1k', '2k'],
      'qualities': const ['low', 'medium'],
      'formats': const ['jpeg'],
      'max_count': 4,
      'max_references': 3,
    });

    expect(caps.sizes, ['1024x1024', '1536x1024']);
    expect(caps.aspectRatios, ['1:1', '16:9']);
    expect(caps.resolutions, ['1k', '2k']);
    expect(caps.qualities, ['low', 'medium']);
    expect(caps.maxCount, 4);
    expect(caps.maxReferences, 3);
    expect(caps.isEmpty, isFalse);
  });

  test('image capabilities 空上报视为未配置', () {
    final caps = ImageGenerationCapabilities.fromJson(const {});
    expect(caps.isEmpty, isTrue);
  });

  test('stripPromptArgs 剥掉用户气泡里的生图参数尾巴', () {
    expect(
      ImageGenerationOptions.stripPromptArgs('画一只猫\n--n=3 --size=1024x1024'),
      '画一只猫',
    );
    expect(
      ImageGenerationOptions.stripPromptArgs(
        '画一只猫 --aspect-ratio=16:9 --quality=low',
      ),
      '画一只猫',
    );
    expect(
      ImageGenerationOptions.stripPromptArgs('普通对话 --n=3'),
      '普通对话',
    );
    // 没有参数的原文保持不变。
    expect(
      ImageGenerationOptions.stripPromptArgs('画一只猫'),
      '画一只猫',
    );
    // 值不是生图参数形态的正文不被误伤：中文、句子、其他 flag。
    expect(
      ImageGenerationOptions.stripPromptArgs('--n=三张才可以哦'),
      '--n=三张才可以哦',
    );
    expect(
      ImageGenerationOptions.stripPromptArgs('--quality=帮我调高质量'),
      '--quality=帮我调高质量',
    );
    expect(
      ImageGenerationOptions.stripPromptArgs('跑一下 --verbose 模式'),
      '跑一下 --verbose 模式',
    );
  });

  test('stripPromptArgs 覆盖视频参数与单独一行参数', () {
    expect(
      ImageGenerationOptions.stripPromptArgs('拍一段海浪\n--duration=8 --aspect-ratio=16:9'),
      '拍一段海浪',
    );
    // 参数独占一行、后面接空行，也要剥干净，不留空段。
    expect(
      ImageGenerationOptions.stripPromptArgs('画一只狗\n\n--n=2'),
      '画一只狗',
    );
    // 只有参数、没有正文时返回空串。
    expect(ImageGenerationOptions.stripPromptArgs('--n=2'), '');
  });

  test('AiOutputFile 区分视频与图片结果', () {
    final video = AiOutputFile.fromJson({
      'id': 'file_clip',
      'filename': 'grok-imagine-video-1.5.mp4',
      'mime': 'video/mp4',
      'url': 'https://example.com/v.mp4',
    });
    expect(video.isVideo, isTrue);
    expect(video.isImage, isFalse);

    final image = AiOutputFile.fromJson({
      'id': 'file_pic',
      'filename': 'shot.png',
      'mime': 'image/png',
      'url': 'https://example.com/p.png',
    });
    expect(image.isImage, isTrue);
    expect(image.isVideo, isFalse);
  });
}
