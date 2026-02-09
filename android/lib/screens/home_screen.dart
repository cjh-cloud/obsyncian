import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../providers/app_state.dart';
import '../services/sync_orchestrator.dart';

/// The single screen of the Obsyncian app.
///
/// Layout:
///   - **Unconfigured**: prominent "Select config.json" card
///   - **Configured**: config summary, sync status indicator, sync log, manual
///     sync FAB
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final ScrollController _scrollController = ScrollController();

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Obsyncian'),
        actions: [
          // Connectivity indicator
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Icon(
              appState.isOnline ? Icons.wifi : Icons.wifi_off,
              color: appState.isOnline
                  ? theme.colorScheme.primary
                  : theme.colorScheme.error,
            ),
          ),
          if (appState.isConfigured)
            PopupMenuButton<String>(
              onSelected: (value) {
                if (value == 'change_config') {
                  appState.clearConfig();
                }
              },
              itemBuilder: (_) => [
                const PopupMenuItem(
                  value: 'change_config',
                  child: Text('Change config'),
                ),
              ],
            ),
        ],
      ),
      body: appState.needsVaultPath
          ? _buildVaultPickerView(context, appState, theme)
          : appState.isConfigured
              ? _buildConfiguredView(context, appState, theme)
              : _buildUnconfiguredView(context, appState, theme),
      floatingActionButton: appState.isConfigured
          ? FloatingActionButton.extended(
              onPressed: appState.syncState == SyncState.syncing
                  ? null
                  : () => appState.triggerSync(),
              icon: appState.syncState == SyncState.syncing
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.sync),
              label: Text(appState.syncState == SyncState.syncing
                  ? 'Syncing...'
                  : 'Sync now'),
            )
          : null,
    );
  }

  // ---------------------------------------------------------------------------
  // Unconfigured state — first launch
  // ---------------------------------------------------------------------------

  Widget _buildUnconfiguredView(
      BuildContext context, AppState appState, ThemeData theme) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_sync_outlined,
              size: 96,
              color: theme.colorScheme.primary.withValues(alpha: 0.6),
            ),
            const SizedBox(height: 24),
            Text(
              'Welcome to Obsyncian',
              style: theme.textTheme.headlineMedium?.copyWith(
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 12),
            Text(
              'Select your config.json file to start syncing your Obsidian vault with S3.',
              style: theme.textTheme.bodyLarge?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 32),
            FilledButton.icon(
              onPressed: () => appState.pickConfig(),
              icon: const Icon(Icons.file_open),
              label: const Text('Select config.json'),
              style: FilledButton.styleFrom(
                padding: const EdgeInsets.symmetric(
                  horizontal: 24,
                  vertical: 16,
                ),
                textStyle: theme.textTheme.titleMedium,
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Vault directory picker — config loaded but local path isn't usable
  // ---------------------------------------------------------------------------

  Widget _buildVaultPickerView(
      BuildContext context, AppState appState, ThemeData theme) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.folder_off_outlined,
              size: 96,
              color: theme.colorScheme.error.withValues(alpha: 0.6),
            ),
            const SizedBox(height: 24),
            Text(
              'Vault Directory Needed',
              style: theme.textTheme.headlineMedium?.copyWith(
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 12),
            Text(
              'The config\'s local path isn\'t accessible on this device. '
              'Please select a folder on this device where your Obsidian '
              'vault should be synced.',
              style: theme.textTheme.bodyLarge?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
              textAlign: TextAlign.center,
            ),
            if (appState.config != null) ...[
              const SizedBox(height: 8),
              Text(
                'Original path: ${appState.config!.local}',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.outline,
                  fontFamily: 'monospace',
                ),
                textAlign: TextAlign.center,
              ),
            ],
            const SizedBox(height: 32),
            FilledButton.icon(
              onPressed: () => appState.pickVaultDirectory(),
              icon: const Icon(Icons.folder_open),
              label: const Text('Select vault folder'),
              style: FilledButton.styleFrom(
                padding: const EdgeInsets.symmetric(
                  horizontal: 24,
                  vertical: 16,
                ),
                textStyle: theme.textTheme.titleMedium,
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Configured state — main view
  // ---------------------------------------------------------------------------

  Widget _buildConfiguredView(
      BuildContext context, AppState appState, ThemeData theme) {
    // Auto-scroll to bottom when logs change
    _scrollToBottom();

    return Column(
      children: [
        // Config summary card
        _buildConfigCard(appState, theme),
        // Sync status
        _buildSyncStatusBar(appState, theme),
        // Log output
        Expanded(
          child: _buildLogView(appState, theme),
        ),
      ],
    );
  }

  Widget _buildConfigCard(AppState appState, ThemeData theme) {
    final config = appState.config!;
    return Card(
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 4),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.folder_outlined,
                    size: 20, color: theme.colorScheme.primary),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    config.local,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      fontWeight: FontWeight.w500,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Icon(Icons.cloud_outlined,
                    size: 20, color: theme.colorScheme.primary),
                const SizedBox(width: 8),
                Text(
                  's3://${config.cloud}',
                  style: theme.textTheme.bodyMedium,
                ),
                const Spacer(),
                Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.secondaryContainer,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(
                    config.awsRegion,
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: theme.colorScheme.onSecondaryContainer,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildSyncStatusBar(AppState appState, ThemeData theme) {
    final (icon, label, color) = switch (appState.syncState) {
      SyncState.idle => (
          Icons.check_circle_outline,
          'Idle',
          theme.colorScheme.primary,
        ),
      SyncState.syncing => (
          Icons.sync,
          'Syncing...',
          theme.colorScheme.tertiary,
        ),
      SyncState.error => (
          Icons.error_outline,
          'Error',
          theme.colorScheme.error,
        ),
      SyncState.offline => (
          Icons.cloud_off,
          'Offline',
          theme.colorScheme.outline,
        ),
    };

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
      child: Row(
        children: [
          Icon(icon, size: 16, color: color),
          const SizedBox(width: 8),
          Text(
            label,
            style: theme.textTheme.labelMedium?.copyWith(color: color),
          ),
          if (!appState.isOnline) ...[
            const Spacer(),
            Icon(Icons.wifi_off, size: 14, color: theme.colorScheme.error),
            const SizedBox(width: 4),
            Text(
              'No connection',
              style: theme.textTheme.labelSmall
                  ?.copyWith(color: theme.colorScheme.error),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildLogView(AppState appState, ThemeData theme) {
    final logs = appState.logs;

    if (logs.isEmpty) {
      return Center(
        child: Text(
          'No sync activity yet.',
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      );
    }

    return ListView.builder(
      controller: _scrollController,
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 80),
      itemCount: logs.length,
      itemBuilder: (context, index) {
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: 1),
          child: Text(
            logs[index],
            style: theme.textTheme.bodySmall?.copyWith(
              fontFamily: 'monospace',
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        );
      },
    );
  }
}
