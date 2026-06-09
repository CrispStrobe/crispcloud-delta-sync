# CrispCloud Delta Sync

**Block-level delta sync for Nextcloud and ownCloud.** Upload only the blocks that changed instead of re-uploading entire files.

Ideal for large files that change incrementally: VeraCrypt/LUKS containers, database files, disk images, VM snapshots, PST archives, virtual hard drives.

## What's in this repo

```
server/     PHP app — Nextcloud 25–33 and ownCloud 10.11+
ocis/       Go sidecar — ownCloud Infinite Scale (oCIS v5+)
client/     Dart CLI demo — exercises all 4 API endpoints
```

## Quick start

### 1. Install the server app

```bash
# Copy to your Nextcloud apps directory
cp -r server/ /path/to/nextcloud/apps/crispcloud_delta

# Enable
cd /path/to/nextcloud
sudo -u www-data php occ app:enable crispcloud_delta

# Verify
curl -u admin:password http://localhost/index.php/apps/crispcloud_delta/api/status
```

### 2. Try the CLI demo

```bash
cd client/
dart pub get

# Check server status
dart run bin/delta_sync_cli.dart status \
  -u http://localhost:8888 --user admin --pass secret

# Compute local block map
dart run bin/delta_sync_cli.dart compute -f /path/to/large-file.bin

# Fetch remote block map
dart run bin/delta_sync_cli.dart blockmap \
  -u http://localhost:8888 --user admin --pass secret \
  --path Documents/large-file.bin

# Delta sync — uploads only changed blocks
dart run bin/delta_sync_cli.dart sync \
  -u http://localhost:8888 --user admin --pass secret \
  -f /local/large-file.bin --path Documents/large-file.bin
```

## How it works

1. Files are split into fixed 4 MB blocks
2. Each block gets two hashes: **Adler-32** (fast weak hash) + **SHA-256** (collision-resistant strong hash)
3. Client compares its local block map against the server's cached block map
4. Only blocks where hashes differ are uploaded via the REST API
5. After all block writes, a finalize call updates the file's mtime and refreshes the cached block map

A 500 MB VeraCrypt container where 8 MB changed → **98.4% bandwidth savings** (only 2 blocks uploaded instead of 125).

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/status` | Health check (public) |
| `GET` | `/api/blockmap/{path}` | Get block map (auto-computed, cached by ETag) |
| `POST` | `/api/blocks/{path}?offset=N&size=M` | Write a single block at offset |
| `POST` | `/api/finalize/{path}?size=N` | Finalize after block writes (truncates file to N bytes if N < current size) |

## Testing

Live integration test suites are included for all three platforms:

```bash
# Nextcloud
python server/test_live.py http://your-server:8888 admin password

# ownCloud 10
python server/test_live.py http://your-server:8889 admin password

# oCIS (Go sidecar + oCIS running — see ocis/README.md for setup)
python server/test_ocis_live.py \
    http://your-server:8091 \
    https://your-server:9200 \
    admin password
```

Each suite covers 5 scenarios: status endpoint, block map accuracy, bit-perfect
delta update, file shrink (truncation), and file grow. All 8 assertions pass on
Nextcloud 33, ownCloud 10, and oCIS v8.0.4.

## Compatibility

| Platform | Supported |
|----------|-----------|
| Nextcloud 25–33 | Yes (tested on NC 33 / PHP 8.3) |
| ownCloud 10.11+ | Yes (tested on OC 10.15 / PHP 7.4) |
| ownCloud Infinite Scale (oCIS) | Yes (Go sidecar — tested on oCIS v8.0.4) |

**Requirements:** PHP 7.4+. Uses only stable OCP APIs shared by both platforms.

## Clients

- **[CrispCloud](https://github.com/CrispStrobe/CrispCloud)** — full-featured cross-platform file manager with built-in delta sync support for Nextcloud, pCloud, and S3
- **[Nextcloud desktop client](https://github.com/CrispStrobe/nextcloud-desktop/releases/tag/delta-sync-latest)** — fork with delta sync, settings UI, activity display, notifications. Pre-built binaries for Linux, Windows, macOS.
- **[ownCloud desktop client](https://github.com/CrispStrobe/owncloud-client/releases/tag/delta-sync-latest)** — fork with delta sync, settings UI, activity display. Pre-built binaries for Linux, Windows, macOS.
- **This repo's CLI demo** — minimal reference implementation

## oCIS (ownCloud Infinite Scale)

A Go-based sidecar extension is available in [`ocis/`](ocis/) for ownCloud
Infinite Scale. It exposes the same four-endpoint API as the PHP app so
existing desktop clients work against oCIS without modification.

File I/O is proxied through oCIS's own WebDAV endpoint (all storage backends
supported). Authentication is fully delegated: the client's OIDC Bearer token
passes through to oCIS on every call.

```
server/     PHP app — Nextcloud 25–33 and ownCloud 10.11+
ocis/       Go sidecar — ownCloud Infinite Scale (oCIS v5+)
client/     Dart CLI demo
```

See [`ocis/README.md`](ocis/README.md) for deployment instructions.

## Algorithm

The block map format is compatible with rsync-style delta transfer:

```json
{
  "filePath": "/Documents/vault.vc",
  "totalSize": 524288000,
  "blockSize": 4194304,
  "blockCount": 125,
  "signatures": [
    {
      "blockIndex": 0,
      "offset": 0,
      "size": 4194304,
      "weakHash": 1234567890,
      "strongHash": "a1b2c3d4e5f6..."
    }
  ],
  "createdAt": "2026-06-08T12:00:00+00:00",
  "etag": "abc123def456"
}
```

- **Adler-32** (RFC 1950) — fast O(n) checksum with O(1) rolling update capability
- **SHA-256** — collision-resistant confirmation, only computed when weak hashes match
- **4 MB blocks** — balances granularity vs overhead; configurable in future versions

## License

AGPL-3.0 — same as Nextcloud and CrispCloud.
