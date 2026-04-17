/**
 * @format
 */

// Hermes does not provide crypto, ReadableStream, or URL — all needed by AWS SDK v3.
import { install } from 'react-native-quick-crypto';
install();

import { ReadableStream } from 'web-streams-polyfill';
globalThis.ReadableStream = globalThis.ReadableStream || ReadableStream;

import 'react-native-url-polyfill/auto';

if (typeof globalThis.structuredClone === 'undefined') {
  globalThis.structuredClone = (obj) => JSON.parse(JSON.stringify(obj));
}

import { AppRegistry } from 'react-native';
import App from './App';
import { name as appName } from './app.json';

AppRegistry.registerComponent(appName, () => App);
