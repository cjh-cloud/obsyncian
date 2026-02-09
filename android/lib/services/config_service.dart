import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/obsyncian_config.dart';

/// Manages loading and persisting the path to the config.json file.
///
/// On first launch the user picks a config.json via the system file picker.
/// The chosen path is stored in SharedPreferences so subsequent launches
/// load it automatically.
class ConfigService {
  static const _configPathKey = 'obsyncian_config_path';
  static const _vaultPathKey = 'obsyncian_vault_path';

  /// Try to load the config from the previously saved path.
  /// Returns null if no path was saved or the file no longer exists.
  Future<ObsyncianConfig?> loadSavedConfig() async {
    final prefs = await SharedPreferences.getInstance();
    final path = prefs.getString(_configPathKey);
    if (path == null) return null;
    return _readConfigFile(path);
  }

  /// Get the saved config file path, if any.
  Future<String?> getSavedConfigPath() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_configPathKey);
  }

  /// Open a file picker so the user can select their config.json.
  /// Returns the parsed config, or null if the user cancelled.
  Future<ObsyncianConfig?> pickConfigFile() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.any,
      dialogTitle: 'Select your Obsyncian config.json',
    );

    if (result == null || result.files.isEmpty) return null;

    final path = result.files.single.path;
    if (path == null) return null;

    final config = await _readConfigFile(path);
    if (config != null) {
      await _saveConfigPath(path);
    }
    return config;
  }

  /// Clear the saved config and vault paths (e.g. to allow re-picking).
  Future<void> clearSavedConfig() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_configPathKey);
    await prefs.remove(_vaultPathKey);
  }

  // ---------------------------------------------------------------------------
  // Vault directory (Android-local override for the config's `local` field)
  // ---------------------------------------------------------------------------

  /// Get the saved Android vault directory path, if any.
  Future<String?> getSavedVaultPath() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_vaultPathKey);
  }

  /// Save the Android vault directory path.
  Future<void> saveVaultPath(String path) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_vaultPathKey, path);
  }

  /// Open a directory picker so the user can choose where to store the vault
  /// on this Android device.
  Future<String?> pickVaultDirectory() async {
    final result = await FilePicker.platform.getDirectoryPath(
      dialogTitle: 'Select your local vault folder',
    );
    if (result == null) return null;

    await saveVaultPath(result);
    return result;
  }

  Future<ObsyncianConfig?> _readConfigFile(String path) async {
    try {
      final file = File(path);
      if (!await file.exists()) return null;
      var contents = await file.readAsString();
      // Strip UTF-8 BOM if present (common on Windows-created files)
      if (contents.startsWith('\uFEFF')) {
        contents = contents.substring(1);
      }
      final json = jsonDecode(contents) as Map<String, dynamic>;
      return ObsyncianConfig.fromJson(json);
    } catch (e) {
      return null;
    }
  }

  Future<void> _saveConfigPath(String path) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_configPathKey, path);
  }
}
