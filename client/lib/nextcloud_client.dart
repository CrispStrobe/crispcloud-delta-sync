// lib/nextcloud_client.dart
//
// HTTP client for the CrispCloud Delta Sync Nextcloud/ownCloud server app.
// Exercises all 4 REST endpoints:
//   GET  /api/status
//   GET  /api/blockmap/{path}
//   POST /api/blocks/{path}?offset=N&size=M
//   POST /api/finalize/{path}

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'delta_sync.dart';

class NextcloudDeltaSyncClient {
  final String baseUrl;
  final String username;
  final String password;

  NextcloudDeltaSyncClient({
    required this.baseUrl,
    required this.username,
    required this.password,
  });

  String get _appBase {
    final base = baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl;
    return '$base/index.php/apps/crispcloud_delta';
  }

  Map<String, String> get _authHeaders => {
        'Authorization':
            'Basic ${base64Encode(utf8.encode('$username:$password'))}',
      };

  /// Check if the server app is installed and reachable.
  Future<Map<String, dynamic>> status() async {
    final resp = await http.get(
      Uri.parse('$_appBase/api/status'),
      headers: _authHeaders,
    );
    if (resp.statusCode != 200) {
      throw HttpException(
          'Status check failed: ${resp.statusCode} ${resp.body}');
    }
    return jsonDecode(resp.body) as Map<String, dynamic>;
  }

  /// Fetch the remote block map for [remotePath].
  Future<BlockMap> getBlockMap(String remotePath) async {
    final encoded = Uri.encodeComponent(remotePath);
    final resp = await http.get(
      Uri.parse('$_appBase/api/blockmap/$encoded'),
      headers: _authHeaders,
    );
    if (resp.statusCode != 200) {
      throw HttpException(
          'getBlockMap failed: ${resp.statusCode} ${resp.body}');
    }
    // Untrusted server body: jsonDecode already throws FormatException on
    // invalid JSON; guard the object shape so a non-object payload surfaces the
    // same clean error instead of a TypeError from the cast.
    final decoded = jsonDecode(resp.body);
    if (decoded is! Map<String, dynamic>) {
      throw const FormatException(
          'getBlockMap: response body is not a JSON object');
    }
    return BlockMap.fromJson(decoded);
  }

  /// Upload a single block at [offset].
  Future<void> putBlock(
      String remotePath, int offset, Uint8List data) async {
    final encoded = Uri.encodeComponent(remotePath);
    final uri = Uri.parse(
        '$_appBase/api/blocks/$encoded?offset=$offset&size=${data.length}');
    final resp = await http.post(
      uri,
      headers: {
        ..._authHeaders,
        'Content-Type': 'application/octet-stream',
      },
      body: data,
    );
    if (resp.statusCode != 200) {
      throw HttpException('putBlock failed: ${resp.statusCode} ${resp.body}');
    }
  }

  /// Finalize after block writes — triggers mtime update and block map refresh.
  /// Pass [size] to truncate the file if it shrank.
  Future<void> finalize(String remotePath, {int? size}) async {
    final encoded = Uri.encodeComponent(remotePath);
    final sizeParam = size != null ? '?size=$size' : '';
    final resp = await http.post(
      Uri.parse('$_appBase/api/finalize/$encoded$sizeParam'),
      headers: _authHeaders,
    );
    if (resp.statusCode != 200) {
      throw HttpException(
          'finalize failed: ${resp.statusCode} ${resp.body}');
    }
  }

  /// Full delta sync workflow: compute local block map, fetch remote, diff,
  /// upload only changed blocks, finalize.
  ///
  /// Returns the [DeltaResult] showing what changed.
  Future<DeltaResult> deltaUpload(
    String localPath,
    String remotePath, {
    void Function(String message)? log,
  }) async {
    final engine = DeltaSyncEngine();

    log?.call('Computing local block map...');
    final localMap = await engine.computeBlockMap(localPath,
        onProgress: (done, total) {
      log?.call('  Block $done/$total');
    });

    log?.call('Fetching remote block map...');
    final remoteMap = await getBlockMap(remotePath);

    log?.call('Comparing block maps...');
    final delta = engine.compareBlockMaps(localMap, remoteMap);

    log?.call(
        'Changed: ${delta.changedBlocks.length}/${delta.totalBlocks} blocks '
        '(${delta.changedBytes} bytes, '
        '${delta.savingsPercent.toStringAsFixed(1)}% savings)');

    if (delta.changedBlocks.isEmpty) {
      log?.call('File is identical — nothing to upload.');
      return delta;
    }

    // Upload only changed blocks
    final file = File(localPath);
    final raf = await file.open(mode: FileMode.read);
    try {
      for (final blockIdx in delta.changedBlocks) {
        final sig = localMap.signatures[blockIdx];
        await raf.setPosition(sig.offset);
        final buf = Uint8List(sig.size);
        await raf.readInto(buf);
        log?.call('  Uploading block #$blockIdx '
            '(offset=${sig.offset}, size=${sig.size})');
        await putBlock(remotePath, sig.offset, buf);
      }
    } finally {
      await raf.close();
    }

    log?.call('Finalizing...');
    final localSize = await File(localPath).length();
    await finalize(remotePath, size: localSize);
    log?.call('Done.');

    return delta;
  }
}
