# CrispCloud Delta Sync — Nextcloud/ownCloud Server App

Block-level delta sync for large files. Instead of re-uploading an entire 500 MB VeraCrypt container when only 8 MB of blocks changed, this app maintains block-level indexes and accepts partial block updates.

## How it works

1. **Block-level indexes** — Adler-32 weak hash + SHA-256 strong hash per 4 MB block, cached per file
2. **REST API for block maps** — clients compare local vs remote block maps to find diffs
3. **Partial block writes** — only changed blocks are uploaded, written at their exact file offset
4. **Auto-recompute on change** — if the file's ETag changes (edited by another client), the block map is recomputed

## Installation

### From release tarball

```bash
cd /path/to/nextcloud/apps
tar xzf crispcloud_delta-0.1.0.tar.gz
cd /path/to/nextcloud
sudo -u www-data php occ app:enable crispcloud_delta
```

### From source

```bash
cp -r server/ /path/to/nextcloud/apps/crispcloud_delta
cd /path/to/nextcloud
sudo -u www-data php occ app:enable crispcloud_delta
```

### Verify installation

```bash
curl -u admin:password http://localhost/index.php/apps/crispcloud_delta/api/status
# {"app":"crispcloud_delta","version":"0.1.0","status":"ok","blockSize":4194304,"algorithm":"adler32+sha256"}
```

## API Endpoints

All endpoints require authentication (same credentials as WebDAV).

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/blockmap/{path}` | Get block map (auto-computed, cached by ETag) |
| `POST` | `/api/blocks/{path}?offset=N&size=M` | Write a single block at offset |
| `POST` | `/api/finalize/{path}` | Finalize after block writes (touch mtime, recompute) |
| `GET` | `/api/status` | Health check (public, no auth required) |

## Block Map Format

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

## Compatibility

| Platform | Supported | Notes |
|----------|-----------|-------|
| **Nextcloud 25+** | Yes | Primary target |
| **ownCloud 10.11+** | Yes | Shared OCP AppFramework + Files API |
| **ownCloud Infinite Scale (oCIS)** | No | Go-based, no PHP apps |

### Requirements

- PHP 8.0+
- Uses only stable OCP APIs shared by both Nextcloud and ownCloud 10

## Building a release

```bash
cd server/
make release
# Creates build/crispcloud_delta-0.1.0.tar.gz
```

## Nextcloud App Store Submission

1. Create an account at https://apps.nextcloud.com
2. Register app ID `crispcloud_delta`
3. Generate a signing key and certificate
4. Sign the tarball:
   ```bash
   openssl dgst -sha512 -sign ~/.nextcloud/certificates/crispcloud_delta.key \
     build/crispcloud_delta-0.1.0.tar.gz | openssl base64 -A
   ```
5. Upload at https://apps.nextcloud.com/developer/apps/releases/new

## License

AGPL-3.0
