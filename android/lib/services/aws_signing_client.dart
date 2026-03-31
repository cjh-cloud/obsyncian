import 'dart:typed_data';

import 'package:aws_common/aws_common.dart';
import 'package:aws_signature_v4/aws_signature_v4.dart';
import 'package:http/http.dart' as http;

/// Custom HTTP client that re-signs AWS requests using the official
/// `aws_signature_v4` package.
///
/// The auto-generated `aws_*_api` packages rely on `shared_aws_api`'s built-in
/// SigV4 implementation, which has known signing bugs that produce
/// `SignatureDoesNotMatch` errors. This client sits between the API layer and
/// the real HTTP client, stripping the old (incorrect) signing headers and
/// re-signing with the official AWS SigV4 signer.
class AwsSigningClient extends http.BaseClient {
  final http.Client _inner;
  final AWSSigV4Signer _signer;
  final String _region;
  final AWSService _service;
  final ServiceConfiguration _serviceConfig;

  /// Creates a signing client for a specific AWS service.
  ///
  /// [service] should be the AWS service signing name, e.g. `'s3'`,
  /// `'dynamodb'`, `'sqs'`, `'sns'`.
  AwsSigningClient({
    required String accessKey,
    required String secretKey,
    required String region,
    required String service,
    http.Client? inner,
  })  : _inner = inner ?? http.Client(),
        _region = region,
        _service = AWSService(service),
        _serviceConfig = service == 's3'
            ? S3ServiceConfiguration()
            : const BaseServiceConfiguration(),
        _signer = AWSSigV4Signer(
          credentialsProvider: AWSCredentialsProvider(
            AWSCredentials(accessKey, secretKey),
          ),
        );

  /// Header names (lowercase) that belong to the SigV4 signing process
  /// and must be stripped before re-signing.
  static const _signingHeaders = {
    'authorization',
    'x-amz-date',
    'host',
    'x-amz-content-sha256',
    'x-amz-security-token',
  };

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    // 1. Extract body bytes from the incoming (incorrectly-signed) request.
    final Uint8List bodyBytes;
    if (request is http.Request) {
      bodyBytes = request.bodyBytes;
    } else {
      bodyBytes = Uint8List(0);
    }

    // 2. Collect application headers, skipping any signing-related headers
    //    that were added by shared_aws_api's signer.
    final cleanHeaders = <String, String>{};
    for (final entry in request.headers.entries) {
      if (_signingHeaders.contains(entry.key.toLowerCase())) continue;
      cleanHeaders[entry.key] = entry.value;
    }

    // 3. Build an AWSHttpRequest for the official signer.
    final awsRequest = AWSHttpRequest(
      method: AWSHttpMethod.fromString(request.method),
      uri: request.url,
      headers: cleanHeaders,
      body: bodyBytes,
    );

    // 4. Sign with the official aws_signature_v4 signer.
    final scope = AWSCredentialScope(
      region: _region,
      service: _service,
    );

    final signedRequest = await _signer.sign(
      awsRequest,
      credentialScope: scope,
      serviceConfiguration: _serviceConfig,
    );

    // 5. Create a fresh http.Request with the correctly-signed headers.
    final outgoing = http.Request(request.method, signedRequest.uri);
    outgoing.bodyBytes = bodyBytes;
    outgoing.headers.addAll(signedRequest.headers);

    return _inner.send(outgoing);
  }

  @override
  void close() {
    _inner.close();
  }
}
