// lib/delta_sync.dart
//
// Standalone delta sync library — computes block maps (Adler-32 + SHA-256),
// compares them, and produces transfer plans.  Extracted from the CrispCloud
// file manager for independent use.

import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const int kDefaultBlockSize = 4 * 1024 * 1024; // 4 MB
const int _kAdlerMod = 65521; // Largest prime < 2^16

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

class BlockSignature {
  final int blockIndex;
  final int offset;
  final int size;
  final int weakHash;
  final String strongHash;

  const BlockSignature({
    required this.blockIndex,
    required this.offset,
    required this.size,
    required this.weakHash,
    required this.strongHash,
  });

  Map<String, dynamic> toJson() => {
        'blockIndex': blockIndex,
        'offset': offset,
        'size': size,
        'weakHash': weakHash,
        'strongHash': strongHash,
      };

  factory BlockSignature.fromJson(Map<String, dynamic> j) => BlockSignature(
        blockIndex: j['blockIndex'] as int,
        offset: j['offset'] as int,
        size: j['size'] as int,
        weakHash: j['weakHash'] as int,
        strongHash: j['strongHash'] as String,
      );

  @override
  bool operator ==(Object other) =>
      other is BlockSignature &&
      blockIndex == other.blockIndex &&
      weakHash == other.weakHash &&
      strongHash == other.strongHash;

  @override
  int get hashCode => Object.hash(blockIndex, weakHash, strongHash);
}

class BlockMap {
  final String filePath;
  final int totalSize;
  final int blockSize;
  final int blockCount;
  final List<BlockSignature> signatures;
  final DateTime createdAt;
  final String? etag;

  const BlockMap({
    required this.filePath,
    required this.totalSize,
    required this.blockSize,
    required this.blockCount,
    required this.signatures,
    required this.createdAt,
    this.etag,
  });

  Map<String, dynamic> toJson() => {
        'filePath': filePath,
        'totalSize': totalSize,
        'blockSize': blockSize,
        'blockCount': blockCount,
        'signatures': signatures.map((s) => s.toJson()).toList(),
        'createdAt': createdAt.toIso8601String(),
        if (etag != null) 'etag': etag,
      };

  factory BlockMap.fromJson(Map<String, dynamic> j) => BlockMap(
        filePath: j['filePath'] as String,
        totalSize: j['totalSize'] as int,
        blockSize: j['blockSize'] as int,
        blockCount: j['blockCount'] as int,
        signatures: (j['signatures'] as List<dynamic>)
            .map((e) => BlockSignature.fromJson(e as Map<String, dynamic>))
            .toList(),
        createdAt: DateTime.parse(j['createdAt'] as String),
        etag: j['etag'] as String?,
      );
}

class DeltaResult {
  final int totalBlocks;
  final List<int> changedBlocks;
  final int totalBytes;
  final int changedBytes;

  const DeltaResult({
    required this.totalBlocks,
    required this.changedBlocks,
    required this.totalBytes,
    required this.changedBytes,
  });

  double get savingsPercent {
    if (totalBytes == 0) return 100.0;
    return ((totalBytes - changedBytes) / totalBytes) * 100.0;
  }
}

// ---------------------------------------------------------------------------
// DeltaSyncEngine
// ---------------------------------------------------------------------------

class DeltaSyncEngine {
  /// Adler-32 checksum (RFC 1950).
  int adler32(List<int> data) {
    int a = 1, b = 0;
    for (final byte in data) {
      a = (a + (byte & 0xFF)) % _kAdlerMod;
      b = (b + a) % _kAdlerMod;
    }
    return (b << 16) | a;
  }

  /// SHA-256 hex digest.
  String sha256Hex(List<int> data) => sha256.convert(data).toString();

  /// Compute block map for a local file.
  Future<BlockMap> computeBlockMap(
    String filePath, {
    int blockSize = kDefaultBlockSize,
    void Function(int done, int total)? onProgress,
  }) async {
    final file = File(filePath);
    final fileSize = await file.length();
    final blockCount =
        fileSize == 0 ? 0 : ((fileSize + blockSize - 1) ~/ blockSize);
    final signatures = <BlockSignature>[];

    if (fileSize > 0) {
      final raf = await file.open(mode: FileMode.read);
      try {
        for (int i = 0; i < blockCount; i++) {
          final offset = i * blockSize;
          final actualSize = (offset + blockSize > fileSize)
              ? (fileSize - offset)
              : blockSize;
          final buf = Uint8List(actualSize);
          await raf.readInto(buf);
          signatures.add(BlockSignature(
            blockIndex: i,
            offset: offset,
            size: actualSize,
            weakHash: adler32(buf),
            strongHash: sha256Hex(buf),
          ));
          onProgress?.call(i + 1, blockCount);
        }
      } finally {
        await raf.close();
      }
    }

    return BlockMap(
      filePath: filePath,
      totalSize: fileSize,
      blockSize: blockSize,
      blockCount: blockCount,
      signatures: signatures,
      createdAt: DateTime.now(),
    );
  }

  /// Compare local and remote block maps.
  DeltaResult compareBlockMaps(BlockMap local, BlockMap remote) {
    final remoteByIndex = <int, BlockSignature>{
      for (final s in remote.signatures) s.blockIndex: s,
    };
    final changed = <int>[];
    int changedBytes = 0;

    for (final sig in local.signatures) {
      final remoteSig = remoteByIndex[sig.blockIndex];
      if (remoteSig == null ||
          sig.weakHash != remoteSig.weakHash ||
          sig.strongHash != remoteSig.strongHash) {
        changed.add(sig.blockIndex);
        changedBytes += sig.size;
      }
    }

    return DeltaResult(
      totalBlocks: local.blockCount,
      changedBlocks: changed,
      totalBytes: local.totalSize,
      changedBytes: changedBytes,
    );
  }
}
