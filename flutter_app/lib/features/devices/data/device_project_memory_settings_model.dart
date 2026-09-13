class DeviceProjectMemorySettingsModel {
  final String model;
  final String variant;

  const DeviceProjectMemorySettingsModel({this.model = '', this.variant = ''});

  factory DeviceProjectMemorySettingsModel.fromJson(
    Map<String, dynamic> json,
  ) => DeviceProjectMemorySettingsModel(
    model: json['model']?.toString() ?? '',
    variant: json['variant']?.toString() ?? '',
  );
}
