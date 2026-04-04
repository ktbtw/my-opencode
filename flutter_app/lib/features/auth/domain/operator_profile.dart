class OperatorProfile {
  OperatorProfile({
    required this.id,
    required this.operatorUid,
    required this.username,
    required this.name,
    required this.operatorKey,
  });

  final int id;
  final String operatorUid;
  final String username;
  final String name;
  final String operatorKey;

  factory OperatorProfile.fromJson(Map<String, dynamic> json) {
    return OperatorProfile(
      id: json['id'] as int? ?? 0,
      operatorUid: json['operator_uid'] as String? ?? '',
      username: json['username'] as String? ?? '',
      name: json['name'] as String? ?? '',
      operatorKey: json['operator_key'] as String? ?? '',
    );
  }
}
