import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/s3_sync_service.dart';

void main() {
  group('SyncDiff', () {
    test('hasChanges is true when there are uploads', () {
      const diff = SyncDiff(toUpload: ['a.md'], toDownload: [], toDelete: []);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is true when there are downloads', () {
      const diff = SyncDiff(toUpload: [], toDownload: ['b.md'], toDelete: []);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is true when there are deletes', () {
      const diff = SyncDiff(toUpload: [], toDownload: [], toDelete: ['c.md']);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is false when empty', () {
      const diff = SyncDiff(toUpload: [], toDownload: [], toDelete: []);
      expect(diff.hasChanges, isFalse);
    });

    test('toString includes counts', () {
      const diff = SyncDiff(
        toUpload: ['a.md', 'b.md'],
        toDownload: ['c.md'],
        toDelete: [],
      );
      expect(diff.toString(), contains('upload: 2'));
      expect(diff.toString(), contains('download: 1'));
      expect(diff.toString(), contains('delete: 0'));
    });
  });
}
