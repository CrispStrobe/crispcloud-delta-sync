# crispcloud-delta — oCIS extension

Block-level delta sync for **ownCloud Infinite Scale (oCIS)**.

Runs as a lightweight Go sidecar alongside oCIS and exposes the same REST API
as the PHP `crispcloud_delta` app for Nextcloud/ownCloud 10, so existing desktop
clients work against oCIS without any client-side changes.

## How it works

```
Desktop client                  This service                  oCIS
──────────────                  ────────────                  ────
GET /api/blockmap/file  ──────► HEAD file        ──────────► oCIS WebDAV
                                stream-GET file  ◄──────────
                                compute hashes
                        ◄────── return block map

POST /api/blocks/file   ──────► buffer block on disk (temp)

POST /api/finalize/file ──────► GET full file    ──────────► oCIS WebDAV
                                apply buffered blocks
                                PUT patched file ──────────► oCIS WebDAV
                        ◄────── "finalized"
```

File I/O goes through oCIS's own WebDAV endpoint, so all storage backends
(local decomposedfs, S3, EOS) are automatically supported.

Authentication is fully delegated to oCIS: the client's Bearer token is passed
through on every WebDAV call, so oCIS validates it and enforces authorisation.

## Requirements

- oCIS v5+ (or any version exposing `/remote.php/webdav/` with Bearer auth)
- Go 1.22+ (build) or Docker (run)

## Quick start

### Docker Compose

Add the extension as a sidecar in your oCIS `docker-compose.yml`:

```yaml
services:
  ocis:
    image: owncloud/ocis:latest
    # ... your existing oCIS config ...

  crispcloud-delta:
    build: https://github.com/CrispStrobe/crispcloud-delta-sync.git#main:ocis
    # or: image: ghcr.io/crispstrobe/crispcloud-delta:latest
    environment:
      OCIS_URL: http://ocis:9200
      LISTEN_ADDR: :8090
      TEMP_DIR: /tmp/crispcloud_delta
    ports:
      - "8090:8090"   # expose to desktop clients
    depends_on:
      - ocis
```

### Build from source

```bash
cd ocis/
go build -o crispcloud-delta .
OCIS_URL=https://your-ocis.example.com ./crispcloud-delta
```

### Verify

```bash
curl http://localhost:8090/api/status
# {"app":"crispcloud_delta","version":"0.1.0","status":"ok",...}
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OCIS_URL` | `http://localhost:9200` | Internal URL of the oCIS instance |
| `LISTEN_ADDR` | `:8090` | TCP address the extension listens on |
| `TEMP_DIR` | `/tmp/crispcloud_delta` | Directory for buffering in-flight blocks |

`TEMP_DIR` must be writable and persistent within a sync session (i.e. survive
across the individual block POST requests until finalize). It is cleaned up
after each successful finalize, so disk usage is bounded by the size of one
file's changed blocks at a time.

## API

Identical to the PHP app — see the top-level README.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/status` | Health check (no auth required) |
| `GET` | `/api/blockmap/{path}` | Block map for a file (cached by ETag) |
| `POST` | `/api/blocks/{path}?offset=N&size=M` | Buffer one block |
| `POST` | `/api/finalize/{path}?size=N` | Apply all blocks and upload to oCIS |

## Desktop client setup

Point the client at the extension's address instead of (or in addition to)
the oCIS address. The extension URL is typically the oCIS host on port 8090:

```
Server URL:       https://your-ocis.example.com      (normal oCIS login)
Delta sync app:   http://your-ocis.example.com:8090   (this extension)
```

The client probes `/api/status` on startup; if it responds it enables delta
sync automatically for files ≥ 10 MB.

## Differences from the PHP app

| | PHP app (NC / OC10) | This Go extension (oCIS) |
|--|---------------------|--------------------------|
| Language | PHP 7.4+ | Go 1.22 |
| File I/O | Direct via OCP\Files API | WebDAV proxied through oCIS |
| Auth | Basic Auth / session | OIDC Bearer token pass-through |
| Block writes | In-place seek+write | Buffered, applied atomically at finalize |
| Cache storage | User's `.crispcloud_delta/` folder | In-memory + temp dir |
| Deploy | `occ app:enable` | Sidecar container / binary |

The block-map JSON format, hash algorithm (Adler-32 + SHA-256), block size
(4 MB), and REST API are identical across both implementations.

## Tests

```bash
go test ./...
```

The `internal/blockmap` package has unit tests covering Adler-32 correctness
(RFC 1950 known values that match the PHP and C++ implementations), block map
construction for single and multi-block files, and ETag passthrough.
