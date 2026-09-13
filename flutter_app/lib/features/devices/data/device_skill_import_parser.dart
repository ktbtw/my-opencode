import 'dart:convert';
import 'dart:typed_data';

import 'package:archive/archive.dart';

import 'device_skill_model.dart';

class DeviceSkillImportParser {
  static DeviceSkillImport parseFile(String name, Uint8List bytes) {
    final lower = name.toLowerCase();
    if (lower.endsWith('.zip')) return _parseZip(bytes);
    return _parseMarkdown(utf8.decode(bytes, allowMalformed: false));
  }

  static DeviceSkillImport _parseZip(Uint8List bytes) {
    final archive = ZipDecoder().decodeBytes(bytes);
    final files = <String, Uint8List>{};
    for (final entry in archive) {
      if (!entry.isFile) continue;
      final path = entry.name
          .replaceAll('\\', '/')
          .replaceFirst(RegExp(r'^/+'), '');
      if (path.isEmpty || path.split('/').contains('..')) continue;
      files[path] = Uint8List.fromList(entry.content as List<int>);
    }
    final skillPath = files.keys.firstWhere(
      (path) => path == 'SKILL.md' || path.endsWith('/SKILL.md'),
      orElse: () => '',
    );
    if (skillPath.isEmpty) {
      throw const FormatException('ZIP 中没有找到 SKILL.md');
    }
    final root = skillPath == 'SKILL.md'
        ? ''
        : skillPath.substring(0, skillPath.length - 'SKILL.md'.length);
    final skill = _parseMarkdown(
      utf8.decode(files[skillPath]!, allowMalformed: false),
    );
    final packageFiles = <DeviceSkillPackageFile>[];
    for (final entry in files.entries) {
      if (entry.key == skillPath || !entry.key.startsWith(root)) continue;
      final relative = entry.key.substring(root.length);
      if (relative.isEmpty || relative == 'skill.json') continue;
      packageFiles.add(
        DeviceSkillPackageFile(
          path: relative,
          content: utf8.decode(entry.value, allowMalformed: false),
        ),
      );
    }
    return DeviceSkillImport(
      name: skill.name,
      description: skill.description,
      content: skill.content,
      packageFiles: packageFiles,
    );
  }

  static DeviceSkillImport _parseMarkdown(String content) {
    final text = content.trim();
    if (text.isEmpty) throw const FormatException('SKILL.md 内容为空');
    var name = '';
    var description = '';
    if (text.startsWith('---')) {
      final end = text.indexOf('\n---', 3);
      if (end >= 0) {
        final frontMatter = text.substring(3, end);
        for (final line in LineSplitter().convert(frontMatter)) {
          final separator = line.indexOf(':');
          if (separator < 0) continue;
          final key = line.substring(0, separator).trim();
          var value = line.substring(separator + 1).trim();
          if ((value.startsWith('"') && value.endsWith('"')) ||
              (value.startsWith("'") && value.endsWith("'"))) {
            value = value.substring(1, value.length - 1);
          }
          if (key == 'name') name = value;
          if (key == 'description') description = value;
        }
      }
    }
    if (name.isEmpty) {
      final heading = RegExp(r'^#\s+(.+)$', multiLine: true).firstMatch(text);
      name = heading?.group(1)?.trim() ?? '';
    }
    if (name.isEmpty) throw const FormatException('SKILL.md 缺少 name');
    return DeviceSkillImport(
      name: name,
      description: description,
      content: text,
    );
  }
}
