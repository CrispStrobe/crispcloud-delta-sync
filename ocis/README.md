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

Authentication is fully delegated to oCIS: the client's Bearer token **or**
Basic Auth credentials are passed through on every WebDAV call unchanged, so
oCIS validates them and enforces authorisation.

> **Tested:** oCIS v8.0.4 — all 8 live integration tests pass.

## Requirements

- oCIS v5+ (or any version exposing `/remote.php/webdav/`)
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

### Bare-metal / single binary

```bash
# Download oCIS (example: v8.0.4)
curl -L -o /usr/local/bin/ocis \
  https://github.com/owncloud/ocis/releases/download/v8.0.4/ocis-8.0.4-linux-amd64
chmod +x /usr/local/bin/ocis

# Initialise (sets admin password, generates TLS cert)
OCIS_CONFIG_DIR=/data/ocis/config \
ocis init --insecure yes --admin-password changeme

# Start oCIS with Basic Auth enabled (useful for legacy clients and testing)
OCIS_CONFIG_DIR=/data/ocis/config \
OCIS_BASE_DATA_PATH=/data/ocis \
OCIS_URL=https://your-server:9200 \
PROXY_ENABLE_BASIC_AUTH=true \
OCIS_INSECURE=true \
ocis server &

# Build and run the sidecar
cd ocis/
go build -o crispcloud-delta .
OCIS_URL=https://your-server:9200 \
OCIS_INSECURE=true \
./crispcloud-delta
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
| `OCIS_INSECURE` | `false` | Set to `true` to skip TLS cert verification (self-signed certs) |

`TEMP_DIR` must be writable and persistent within a sync session (i.e. survive
across the individual block POST requests until finalize). It is cleaned up
after each successful finalize, so disk usage is bounded by the size of one
file's changed blocks at a time.

### Authentication modes

The sidecar accepts both **OIDC Bearer tokens** and **HTTP Basic Auth**
credentials and passes them through to oCIS unchanged. To enable Basic Auth
on the oCIS side set `PROXY_ENABLE_BASIC_AUTH=true` when starting oCIS.

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
| Auth | Basic Auth / session | Bearer token **or** Basic Auth pass-through |
| Block writes | In-place seek+write | Buffered, applied atomically at finalize |
| Cache storage | User's `.crispcloud_delta/` folder | In-memory + temp dir |
| Deploy | `occ app:enable` | Sidecar container / binary |

The block-map JSON format, hash algorithm (Adler-32 + SHA-256), block size
(4 MB), and REST API are identical across both implementations.

## Tests

### Unit tests

```bash
go test ./...
```

Covers Adler-32 correctness (RFC 1950 known values matching the PHP and C++
implementations), block map construction for single and multi-block files, and
ETag passthrough.

### Live integration tests

```bash
# Start oCIS and the sidecar first (see Quick start above), then:
python3 ../server/test_ocis_live.py \
    http://localhost:8091 \
    https://localhost:9200 \
    admin changeme
```

The suite runs 5 scenarios — status, block map accuracy, delta update
(bit-perfect), file shrink (truncation), file grow — against a real oCIS
instance. All 8 assertions pass on oCIS v8.0.4.
