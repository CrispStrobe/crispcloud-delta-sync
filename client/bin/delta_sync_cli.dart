// bin/delta_sync_cli.dart
//
// Minimal CLI demo for CrispCloud Delta Sync.
//
// Usage:
//   dart run bin/delta_sync_cli.dart status --url http://localhost:8888 --user admin --pass secret
//   dart run bin/delta_sync_cli.dart blockmap --url ... --user ... --pass ... --path Documents/vault.vc
//   dart run bin/delta_sync_cli.dart compute --file /local/path/to/file
//   dart run bin/delta_sync_cli.dart sync --url ... --user ... --pass ... --file /local/file --path remote/path

import 'dart:convert';
import 'dart:io';

import 'package:args/args.dart';

import '../lib/delta_sync.dart';
import '../lib/nextcloud_client.dart';

void main(List<String> args) async {
  final parser = ArgParser()
    ..addCommand('status')
    ..addCommand('blockmap')
    ..addCommand('compute')
    ..addCommand('sync');

  // Global options
  parser.addOption('url', abbr: 'u', help: 'Nextcloud/ownCloud base URL');
  parser.addOption('user', help: 'Username');
  parser.addOption('pass', abbr: 'p', help: 'Password');
  parser.addOption('file', abbr: 'f', help: 'Local file path');
  parser.addOption('path', help: 'Remote file path (relative to user root)');
  parser.addOption('block-size',
      help: 'Block size in bytes (default: 4194304)',
      defaultsTo: '4194304');
  parser.addFlag('help', abbr: 'h', negatable: false);

  final results = parser.parse(args);

  if (results['help'] as bool || results.command == null) {
    _printUsage(parser);
    exit(0);
  }

  final command = results.command!.name!;

  switch (command) {
    case 'status':
      await _status(results);
    case 'blockmap':
      await _blockmap(results);
    case 'compute':
      await _compute(results);
    case 'sync':
      await _sync(results);
    default:
      stderr.writeln('Unknown command: $command');
      _printUsage(parser);
      exit(1);
  }
}

void _printUsage(ArgParser parser) {
  print('''
CrispCloud Delta Sync CLI Demo

Commands:
  status     Check if the server app is installed
  blockmap   Fetch remote block map for a file
  compute    Compute local block map for a file
  sync       Delta-sync a local file to the server

Options:
${parser.usage}

Examples:
  # Check server status
  dart run bin/delta_sync_cli.dart status -u http://localhost:8888 --user admin --pass secret

  # Fetch remote block map
  dart run bin/delta_sync_cli.dart blockmap -u http://localhost:8888 --user admin --pass secret --path Documents/vault.vc

  # Compute local block map
  dart run bin/delta_sync_cli.dart compute -f /path/to/local/file

  # Delta sync (upload changed blocks only)
  dart run bin/delta_sync_cli.dart sync -u http://localhost:8888 --user admin --pass secret -f /local/file --path remote/path
''');
}

NextcloudDeltaSyncClient _makeClient(ArgResults results) {
  final url = results['url'] as String?;
  final user = results['user'] as String?;
  final pass = results['pass'] as String?;
  if (url == null || user == null || pass == null) {
    stderr.writeln('Error: --url, --user, and --pass are required');
    exit(1);
  }
  return NextcloudDeltaSyncClient(
      baseUrl: url, username: user, password: pass);
}

Future<void> _status(ArgResults results) async {
  final client = _makeClient(results);
  try {
    final status = await client.status();
    print('Server app status:');
    print(const JsonEncoder.withIndent('  ').convert(status));
  } catch (e) {
    stderr.writeln('Error: $e');
    stderr.writeln(
        '\nIs the crispcloud_delta app installed and enabled on the server?');
    exit(1);
  }
}

Future<void> _blockmap(ArgResults results) async {
  final client = _makeClient(results);
  final path = results['path'] as String?;
  if (path == null) {
    stderr.writeln('Error: --path is required');
    exit(1);
  }

  try {
    final blockMap = await client.getBlockMap(path);
    print('Remote block map for: $path');
    print('  Total size:  ${blockMap.totalSize} bytes');
    print('  Block size:  ${blockMap.blockSize} bytes');
    print('  Block count: ${blockMap.blockCount}');
    print('  ETag:        ${blockMap.etag ?? "N/A"}');
    print('');
    print('Signatures:');
    for (final sig in blockMap.signatures) {
      print('  #${sig.blockIndex}  offset=${sig.offset}  size=${sig.size}  '
          'weak=0x${sig.weakHash.toRadixString(16)}  '
          'strong=${sig.strongHash.substring(0, 16)}...');
    }
  } catch (e) {
    stderr.writeln('Error: $e');
    exit(1);
  }
}

Future<void> _compute(ArgResults results) async {
  final filePath = results['file'] as String?;
  if (filePath == null) {
    stderr.writeln('Error: --file is required');
    exit(1);
  }

  final blockSize = int.parse(results['block-size'] as String);
  final engine = DeltaSyncEngine();

  print('Computing block map for: $filePath');
  final blockMap = await engine.computeBlockMap(
    filePath,
    blockSize: blockSize,
    onProgress: (done, total) {
      stdout.write('\r  Block $done/$total');
    },
  );
  print('');
  print('  Total size:  ${blockMap.totalSize} bytes');
  print('  Block size:  ${blockMap.blockSize} bytes');
  print('  Block count: ${blockMap.blockCount}');
  print('');
  print('Signatures:');
  for (final sig in blockMap.signatures) {
    print('  #${sig.blockIndex}  offset=${sig.offset}  size=${sig.size}  '
        'weak=0x${sig.weakHash.toRadixString(16)}  '
        'strong=${sig.strongHash.substring(0, 16)}...');
  }

  // Also output JSON
  print('');
  print('JSON:');
  print(const JsonEncoder.withIndent('  ').convert(blockMap.toJson()));
}

Future<void> _sync(ArgResults results) async {
  final client = _makeClient(results);
  final filePath = results['file'] as String?;
  final remotePath = results['path'] as String?;
  if (filePath == null || remotePath == null) {
    stderr.writeln('Error: --file and --path are required');
    exit(1);
  }

  try {
    final delta = await client.deltaUpload(
      filePath,
      remotePath,
      log: (msg) => print(msg),
    );
    print('');
    print('Summary:');
    print('  Total blocks:   ${delta.totalBlocks}');
    print('  Changed blocks: ${delta.changedBlocks.length}');
    print('  Changed bytes:  ${delta.changedBytes}');
    print('  Total bytes:    ${delta.totalBytes}');
    print('  Savings:        ${delta.savingsPercent.toStringAsFixed(1)}%');
  } catch (e) {
    stderr.writeln('Error: $e');
    exit(1);
  }
}
