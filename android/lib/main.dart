import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'providers/app_state.dart';
import 'screens/home_screen.dart';
import 'services/background_service.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialise background service (must happen before runApp)
  await BackgroundSyncService.initialise();

  runApp(const ObsyncianApp());
}

class ObsyncianApp extends StatelessWidget {
  const ObsyncianApp({super.key});

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider(
      create: (_) => AppState()..initialise(),
      child: MaterialApp(
        title: 'Obsyncian',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          colorSchemeSeed: const Color(0xFF7C3AED), // purple accent
          useMaterial3: true,
          brightness: Brightness.light,
        ),
        darkTheme: ThemeData(
          colorSchemeSeed: const Color(0xFF7C3AED),
          useMaterial3: true,
          brightness: Brightness.dark,
        ),
        themeMode: ThemeMode.system,
        home: const HomeScreen(),
      ),
    );
  }
}
