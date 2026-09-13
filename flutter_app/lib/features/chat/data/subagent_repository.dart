import '../../../core/config/api_client.dart';
import 'subagent_models.dart';

class SubagentRepository {
  Future<SubagentTreeSnapshot> getTree(String taskId) async {
    final data = await ApiClient.get(
      '/api/tasks/${Uri.encodeComponent(taskId)}/subagents',
    );
    return SubagentTreeSnapshot.fromJson(data);
  }

  Future<void> control({
    required String taskId,
    required String nodeId,
    required String action,
    String instruction = '',
    int? priority,
    Map<String, dynamic>? model,
  }) async {
    await ApiClient.post(
      '/api/tasks/${Uri.encodeComponent(taskId)}/subagents/${Uri.encodeComponent(nodeId)}/control',
      {
        'action': action,
        if (instruction.isNotEmpty) 'instruction': instruction,
        if (priority != null) 'priority': priority,
        if (model != null) 'model': model,
      },
    );
  }

  Future<SubagentLogPage> getLogs(
    String taskId,
    String nodeId, {
    int limit = 50,
    String? cursor,
  }) async {
    final items = <SubagentLogEvent>[];
    var nextCursor = cursor;
    while (true) {
      final page = await getLogsPage(
        taskId,
        nodeId,
        limit: limit,
        cursor: nextCursor,
      );
      items.addAll(page.items);
      if (!page.hasMore) {
        return SubagentLogPage(
          items: items,
          nextCursor: page.nextCursor,
          hasMore: false,
        );
      }
      if (page.nextCursor.isEmpty || page.nextCursor == nextCursor) {
        throw const FormatException('Invalid subagent log page cursor');
      }
      nextCursor = page.nextCursor;
    }
  }

  Future<SubagentLogPage> getLogsPage(
    String taskId,
    String nodeId, {
    int limit = 50,
    String? cursor,
  }) async {
    final query = <String, String>{
      'limit': limit.toString(),
      if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
    };
    final data = await ApiClient.get(
      '/api/tasks/${Uri.encodeComponent(taskId)}/subagents/${Uri.encodeComponent(nodeId)}/logs?${Uri(queryParameters: query).query}',
    );
    final items = (data['events'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map(
          (item) => SubagentLogEvent.fromJson(Map<String, dynamic>.from(item)),
        )
        .toList(growable: false);
    return SubagentLogPage(
      items: items,
      nextCursor: data['next_cursor']?.toString() ?? '',
      hasMore: data['has_more'] == true,
    );
  }
}

class SubagentLogPage {
  final List<SubagentLogEvent> items;
  final String nextCursor;
  final bool hasMore;

  const SubagentLogPage({
    required this.items,
    required this.nextCursor,
    required this.hasMore,
  });
}
