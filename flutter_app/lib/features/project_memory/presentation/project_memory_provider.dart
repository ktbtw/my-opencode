import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/project_memory_model.dart';
import '../data/project_memory_repository.dart';
import '../data/project_memory_settings_model.dart';

typedef ProjectMemoryScopeKey = ({String machineId, String scopeId});

final projectMemoryRepositoryProvider = Provider(
  (ref) => ProjectMemoryRepository(),
);

final projectMemoryOverviewProvider = FutureProvider.autoDispose
    .family<ProjectMemoryOverviewModel, ProjectMemoryScopeKey>((ref, key) {
      return ref
          .watch(projectMemoryRepositoryProvider)
          .getOverview(key.machineId, key.scopeId);
    });

final projectMemorySettingsProvider = FutureProvider.autoDispose
    .family<ProjectMemorySettingsModel, ProjectMemoryScopeKey>((ref, key) {
      return ref
          .watch(projectMemoryRepositoryProvider)
          .getSettings(key.machineId, key.scopeId);
    });
