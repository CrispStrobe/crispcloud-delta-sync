# CrispCloud Delta Sync — Nextcloud/ownCloud Server App

Block-level delta sync for large files. Instead of re-uploading an entire 500 MB VeraCrypt container when only 8 MB of blocks changed, this app maintains block-level indexes and accepts partial block updates.

## How it works

1. **Block-level indexes** — Adler-32 weak hash + SHA-256 strong hash per 4 MB block, cached per file
2. **REST API for block maps** — clients compare local vs remote block maps to find diffs
3. **Partial block writes** — only changed blocks are uploaded, written at their exact file offset
4. **Auto-recompute on change** — if the file's ETag changes (edited by another client), the block map is recomputed
5. **Optional optimistic concurrency** — send `If-Match: "<etag>"` on block
   and finalize requests; stale maps receive HTTP 412 without mutating the file.

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

File and mutation endpoints require authentication (the same credentials as WebDAV).
The `/api/status` health endpoint is public.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/blockmap/{path}` | Get block map (auto-computed, cached by ETag) |
| `POST` | `/api/blocks/{path}?offset=N&size=M` | Write a single block at offset; optional `If-Match` |
| `POST` | `/api/finalize/{path}` | Finalize after block writes (touch mtime, recompute); optional `If-Match` |

When `If-Match` is supplied, the server compares it to the current file ETag
before each mutation. A mismatch returns `412 Precondition Failed`; clients
should discard the stale block map and retry from a newly fetched map.

Changed blocks are staged in a per-file staging area. Finalize applies all
staged blocks with one complete content write, refreshes the cached block map,
and then clears staging. If a provider temporarily reports the file as locked
by an active WebDAV reader, finalize retries for a bounded period; it never
retries stale ETags or unrelated failures. Readers therefore observe either
the complete old file or the complete finalized file, not a partially patched
file.
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

## Live verification and deployment

The current server app passed **12/12 live checks on Nextcloud 33.0.11 / PHP
8.3.36**: block-map hashes, replacement, shrink/grow, stale-ETag rejection,
staged visibility and an eight-reader finalize race. The test runs used
isolated accounts on the prepared instance through a loopback SSH tunnel.
The ownCloud implementation retains its earlier acceptance; it was not rerun
as part of this Nextcloud maintenance refresh.

On a confined PHP-FPM deployment, configure Nextcloud's `tempdirectory` and
the PHP pool's `sys_temp_dir` to the same private directory outside the web
root, writable only by the PHP service account (directory mode `0700`). The
server app's local lock files use PHP's temp directory. A dedicated Nextcloud
pool can also set `upload_tmp_dir` there. Keep that directory within the
storage paths permitted by the existing confinement profile. For the 16 MiB
POST controls in the live suite, `post_max_size` must exceed 16 MiB; the
prepared test pool used 32 MiB. These are per-site settings.

Browsers additionally require exact-origin CORS and an unauthenticated,
empty preflight response on the intended DAV routes. Actual file requests
still require normal authentication. See the
[tested CrispCloud setup guide](https://github.com/CrispStrobe/CrispCloud/blob/main/docs/PROVIDER_SETUP.md#browser-access-webpwa).
The DAV recipe does not automatically enable the OCS or custom delta API
routes; those need their own scoped configuration.

Current limits remain explicit: PHP finalize materializes the file in memory,
so its working set must fit the site's PHP memory budget. The local finalize
lock coordinates processes on one host; multi-host publication and streamed
finalization need separate acceptance. These checks do not certify every
storage backend or PHP deployment.

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
