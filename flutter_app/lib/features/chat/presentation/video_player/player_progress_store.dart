import 'package:shared_preferences/shared_preferences.dart';

/// 记录每个视频的播放位置，用于下次续播。
///
/// 只存位置，不存业务数据；读写失败静默忽略，不影响播放。
class PlayerProgressStore {
  PlayerProgressStore._();

  static String _key(String taskId, String artifactId) =>
      'video_progress_${taskId}_$artifactId';

  static Future<Duration?> read(String taskId, String artifactId) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final seconds = prefs.getInt(_key(taskId, artifactId));
      if (seconds == null || seconds <= 0) return null;
      return Duration(seconds: seconds);
    } catch (_) {
      return null;
    }
  }

  static Future<void> write(
    String taskId,
    String artifactId,
    Duration position,
  ) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      // 每秒都写太频繁：按秒取整后写入即可。
      await prefs.setInt(_key(taskId, artifactId), position.inSeconds);
    } catch (_) {
      // 忽略：记录进度失败不应影响播放。
    }
  }
}
