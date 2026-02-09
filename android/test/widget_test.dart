import 'package:flutter_test/flutter_test.dart';

import 'package:obsyncian/main.dart';

void main() {
  testWidgets('App renders without crashing', (WidgetTester tester) async {
    await tester.pumpWidget(const ObsyncianApp());
    expect(find.text('Obsyncian'), findsOneWidget);
  });
}
