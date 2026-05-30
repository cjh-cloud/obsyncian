import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/sync_notification_service.dart';
import 'package:obsyncian/services/sync_orchestrator.dart';

void main() {
  group('SyncNotificationService.formatCompletionBody', () {
    test('shows "no changes" when nothing changed', () {
      const result = SyncResult.noChanges();
      expect(
        SyncNotificationService.formatCompletionBody(result),
        'No changes',
      );
    });

    test('shows upload count only', () {
      const result = SyncResult(uploaded: 3, downloaded: 0, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '3 uploaded',
      );
    });

    test('shows download count only', () {
      const result = SyncResult(uploaded: 0, downloaded: 2, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '2 downloaded',
      );
    });

    test('shows delete count only', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 1);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '1 deleted',
      );
    });

    test('shows multiple counts joined', () {
      const result = SyncResult(uploaded: 3, downloaded: 2, deleted: 1);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '3 uploaded, 2 downloaded, 1 deleted',
      );
    });

    test('shows upload and download without delete', () {
      const result = SyncResult(uploaded: 1, downloaded: 4, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '1 uploaded, 4 downloaded',
      );
    });
  });
}
