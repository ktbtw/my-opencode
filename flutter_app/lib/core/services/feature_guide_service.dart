import 'package:shared_preferences/shared_preferences.dart';

class FeatureGuideService {
  FeatureGuideService._();

  static const apiConfigGuideKey = 'feature_guide_api_config_v1';
  static const agentCreateGuideKey = 'feature_guide_agent_create_v1';

  static Future<bool> hasSeen(String key) async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(key) ?? false;
  }

  static Future<void> markSeen(String key) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(key, true);
  }
}
