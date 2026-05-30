import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/sync_orchestrator.dart';

void main() {
  group('SyncResult', () {
    test('hadChanges is true when files were uploaded', () {
      const result = SyncResult(uploaded: 3, downloaded: 0, deleted: 0);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is true when files were downloaded', () {
      const result = SyncResult(uploaded: 0, downloaded: 2, deleted: 0);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is true when files were deleted', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 1);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is false when no changes', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 0);
      expect(result.hadChanges, isFalse);
    });

    test('noChanges factory returns zero counts', () {
      const result = SyncResult.noChanges();
      expect(result.uploaded, 0);
      expect(result.downloaded, 0);
      expect(result.deleted, 0);
      expect(result.hadChanges, isFalse);
    });
  });
}
