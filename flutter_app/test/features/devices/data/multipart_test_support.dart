import 'dart:convert';
import 'dart:io';

/// 测试用的 multipart/form-data 解析。
///
/// 上传分块改为 multipart 二进制后，测试服务器不能再把请求体当 UTF-8 JSON
/// 读；这里提供最小可用解析：取出表单字段与名为 chunk 的原始字节。
/// 只覆盖简单形态（无嵌套、无 transfer-encoding），避免为测试引入额外依赖。
class CapturedMultipart {
  /// 请求字段。multipart 请求为字符串值；JSON 请求保留原始类型。
  final Map<String, dynamic> fields;
  final List<int>? chunk;

  const CapturedMultipart(this.fields, this.chunk);
}

/// 以字节形式收集请求体（二进制内容不能按 UTF-8 解码）。
Future<List<int>> collectRequestBytes(HttpRequest request) async {
  final chunks = <List<int>>[];
  await for (final part in request) {
    chunks.add(part);
  }
  return chunks.expand((part) => part).toList(growable: false);
}

/// 解析请求体：multipart 取字段与 chunk 字节，其余按 JSON 解析。
///
/// 注意：非 multipart 的 JSON 请求保留原始类型（数值仍是数值），
/// 只有 multipart 表单字段才需要字符串化——否则会破坏既有断言。
CapturedMultipart parseRequestBody(
  String contentType,
  List<int> raw, {
  bool allowMalformed = true,
}) {
  if (!contentType.toLowerCase().startsWith('multipart/form-data')) {
    final text = utf8.decode(raw, allowMalformed: allowMalformed);
    final decoded = text.isEmpty
        ? <String, dynamic>{}
        : jsonDecode(text) as Map<String, dynamic>;
    return CapturedMultipart(decoded, null);
  }

  final match = RegExp(
    r'boundary=(?:"([^"]+)"|([^;]+))',
  ).firstMatch(contentType);
  final boundary = (match?.group(1) ?? match?.group(2) ?? '').trim();
  final fields = <String, String>{};
  List<int>? chunk;
  if (boundary.isEmpty) return CapturedMultipart(fields, null);

  final delimiter = utf8.encode('--$boundary');
  var index = 0;
  while (index < raw.length) {
    final start = indexOfBytes(raw, delimiter, index);
    if (start < 0) break;
    var cursor = start + delimiter.length;
    if (cursor + 1 < raw.length &&
        raw[cursor] == 0x2d &&
        raw[cursor + 1] == 0x2d) {
      break;
    }
    if (cursor + 1 < raw.length &&
        raw[cursor] == 0x0d &&
        raw[cursor + 1] == 0x0a) {
      cursor += 2;
    }
    final headerEnd = indexOfBytes(raw, const [0x0d, 0x0a, 0x0d, 0x0a], cursor);
    if (headerEnd < 0) break;
    final headers = utf8.decode(raw.sublist(cursor, headerEnd));
    final bodyStart = headerEnd + 4;
    final nextDelimiter = indexOfBytes(raw, delimiter, bodyStart);
    if (nextDelimiter < 0) break;
    var bodyEnd = nextDelimiter;
    if (bodyEnd >= 2 && raw[bodyEnd - 2] == 0x0d && raw[bodyEnd - 1] == 0x0a) {
      bodyEnd -= 2;
    }
    final payload = raw.sublist(bodyStart, bodyEnd);
    final name = RegExp(r'name="([^"]*)"').firstMatch(headers)?.group(1) ?? '';
    if (name == 'chunk') {
      chunk = payload;
    } else if (name.isNotEmpty) {
      fields[name] = utf8.decode(payload, allowMalformed: true);
    }
    index = nextDelimiter;
  }
  return CapturedMultipart(fields, chunk);
}

int indexOfBytes(List<int> haystack, List<int> needle, int from) {
  outer:
  for (var i = from; i <= haystack.length - needle.length; i++) {
    for (var j = 0; j < needle.length; j++) {
      if (haystack[i + j] != needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}
