# Upstream issue drafts

These are starting points. Read them, make them your own, then open the issues.
The Nextcloud AI policy requires PR descriptions to be in your own words throughout
the review process — the same spirit applies to issues.

---

## nextcloud/desktop — Feature request

**Title:** Feature proposal: block-level delta sync for large files (Adler-32 + SHA-256)

**Body:**

Hi,

I've been working with large files that change incrementally — VeraCrypt
containers, database dumps, VM snapshots, PST archives — and for these the
full re-upload on every sync is painful even on a LAN, let alone over a slow
connection. Chunked TUS helps with resumability but still transfers the whole
file.

I've prototyped a block-level delta sync approach and wanted to share it as a
feature proposal before going any further.

### How it works

1. A small companion Nextcloud app (`crispcloud_delta`) exposes four REST
   endpoints: `/api/status`, `/api/blockmap/{path}`, `/api/blocks/{path}`,
   `/api/finalize/{path}`.
2. The app maintains a per-file block map of 4 MB blocks, each hashed with
   Adler-32 (weak, fast) and SHA-256 (strong, collision-resistant).
3. The desktop client computes the same map locally, diffs it against the
   server's map, and uploads only the changed blocks — then calls finalize to
   update the file's mtime/ETag on the server.
4. The finalize request includes the new total size so the server can truncate
   correctly when a file shrinks (otherwise stale bytes remain at the tail).

For a 500 MB VeraCrypt container where 8 MB changed, this means uploading 2
blocks instead of 125 — around 98% bandwidth savings.

### What I've built so far

- Server app: PHP, compatible with Nextcloud 25–33 and ownCloud 10.11+.
  Uses only stable OCP APIs. Source: https://github.com/CrispStrobe/crispcloud-delta-sync
- Desktop client changes: `PropagateUploadFileDelta` class that slots into
  the existing upload propagator. Falls back gracefully to chunked upload if
  the server app is absent. Branch: https://github.com/CrispStrobe/nextcloud-desktop/tree/delta-sync
- Unit tests for the C++ side (Adler-32 correctness against RFC 1950 reference
  values, block map construction, diff logic for shrink/grow/unchanged files).
- Live integration tests (Python) that verify bit-perfect round-trips,
  truncation, and extension on both server platforms.

### Questions before I write a proper PR

- Is this something the team would consider accepting upstream, or is extending
  file sync protocols out of scope for the desktop client repo?
- Are there architectural concerns I should think about? I'm aware the approach
  requires a companion server app — happy to discuss whether capability
  detection belongs in the server capabilities response instead of a probe call.
- Is there a preference for where the fallback threshold (currently 10 MB) and
  block size (4 MB) should live — hardcoded, configfile, or server-advertised?

Happy to open a draft PR if the direction looks right.

---

## owncloud/client — Feature request

**Title:** Feature proposal: block-level delta sync via crispcloud_delta server app

**Body:**

Hi,

Posting this as a feature proposal before writing a formal PR.

The problem: large files that change incrementally (VeraCrypt containers,
databases, disk images) trigger a full re-upload on every sync. Even TUS
resumability doesn't help here — the whole file still transfers.

I've prototyped block-level delta sync: the client computes Adler-32 + SHA-256
hashes per 4 MB block, diffs them against a server-cached map, and uploads only
the changed blocks. A finalize call patches the file and updates its mtime.

### Current state

I have a working implementation on the `delta-sync` branch of my fork:
https://github.com/CrispStrobe/owncloud-client/tree/delta-sync

The changes are:
- New `PropagateUploadFileDelta` class in `src/libsync/`
- Capability detection via the `crispcloud_delta` server app
- Settings toggle in General Settings
- Activity display showing bandwidth savings
- Graceful fallback to TUS/PUT if the server app isn't present
- Unit tests for the pure-logic parts

The server app works on both ownCloud 10.11+ (PHP 7.4, OCP APIs) and oCIS
(Go sidecar via WebDAV), so there's no hard OC10 dependency.

### Before I submit a PR

A few things I wanted to clarify first:

1. **CLA**: I haven't signed the ownCloud CLA yet. Is the process at
   owncloud.com/contribute/join-the-development/contributor-agreement/
   still the right path, or has it changed?

2. **Branch target**: Should this target the OC10-compatible `2.x` branch
   or is there a plan to bring delta sync to the oCIS client (v7.x) as well?
   I have a Go oCIS extension that exposes the same API, so the client changes
   could be adapted for v7.x too.

3. **Scope**: If a full PR is too broad to start, I could break it into
   smaller pieces — the diff algorithm and unit tests first, then the
   propagator integration, then the UI.

Thanks for any guidance.
