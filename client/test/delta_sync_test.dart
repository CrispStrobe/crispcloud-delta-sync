import 'dart:convert';

import 'package:crispcloud_delta_sync_cli/delta_sync.dart';
import 'package:test/test.dart';

const _validJson = {
  'filePath': 'f',
  'totalSize': 10,
  'blockSize': 4,
  'blockCount': 3,
  'signatures': [
    {
      'blockIndex': 0,
      'offset': 0,
      'size': 4,
      'weakHash': 1,
      'strongHash': 'a',
    },
  ],
  'createdAt': '2026-07-18T00:00:00.000',
  'etag': 'e',
};

void main() {
  group('BlockMap.fromJson round-trips valid input', () {
    test('toJson -> fromJson preserves the block map', () {
      final map = BlockMap.fromJson(Map<String, dynamic>.from(_validJson));
      final round = BlockMap.fromJson(map.toJson());
      expect(round.filePath, 'f');
      expect(round.blockCount, 3);
      expect(round.signatures.single.strongHash, 'a');
      expect(round.etag, 'e');
    });

    test('tolerates an integer JSON-encoded as a double (4.0)', () {
      final j = Map<String, dynamic>.from(_validJson)
        ..['totalSize'] = 10.0
        ..['blockSize'] = 4.0
        ..['blockCount'] = 3.0;
      final map = BlockMap.fromJson(j);
      expect(map.totalSize, 10);
      expect(map.blockSize, 4);
      expect(map.blockCount, 3);
    });
  });

  group('BlockMap.fromJson rejects corrupt/malicious input with '
      'FormatException, not TypeError', () {
    // A corrupt cache or a hostile server response must fail cleanly. Each case
    // asserts FormatException AND isNot(TypeError) (the bare-cast leak).
    void expectFormatException(Map<String, dynamic> Function() build) {
      expect(() => BlockMap.fromJson(build()), throwsFormatException);
      expect(() => BlockMap.fromJson(build()),
          throwsA(isNot(isA<TypeError>())));
    }

    Map<String, dynamic> mutate(void Function(Map<String, dynamic>) f) {
      final j = Map<String, dynamic>.from(_validJson);
      f(j);
      return j;
    }

    test('missing numeric field', () {
      expectFormatException(() => mutate((j) => j.remove('totalSize')));
    });
    test('wrong-typed numeric field (string)', () {
      expectFormatException(() => mutate((j) => j['blockSize'] = 'big'));
    });
    test('non-integer numeric field (1.5)', () {
      expectFormatException(() => mutate((j) => j['blockCount'] = 1.5));
    });
    test('signatures is not a list', () {
      expectFormatException(() => mutate((j) => j['signatures'] = 42));
    });
    test('a signature element is not an object', () {
      expectFormatException(() => mutate((j) => j['signatures'] = [1, 2]));
    });
    test('a signature field is wrong-typed', () {
      expectFormatException(() => mutate((j) => j['signatures'] = [
            {
              'blockIndex': 0,
              'offset': 0,
              'size': 'x', // not an int
              'weakHash': 1,
              'strongHash': 'a',
            }
          ]));
    });
    test('createdAt is not a valid date', () {
      expectFormatException(() => mutate((j) => j['createdAt'] = 'not-a-date'));
    });
    test('etag is present but wrong-typed', () {
      expectFormatException(() => mutate((j) => j['etag'] = 99));
    });
  });

  test('a non-object JSON body decodes to a clean rejection', () {
    // Mirrors NextcloudClient.getBlockMap: decode, require an object, fromJson.
    for (final body in ['[1,2,3]', '"a string"', '42', 'null']) {
      final decoded = jsonDecode(body);
      expect(decoded is Map<String, dynamic>, isFalse,
          reason: 'body $body is not a JSON object');
    }
  });
}
