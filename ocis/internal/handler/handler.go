// Package handler implements the crispcloud_delta REST API for oCIS.
// The four endpoints are identical to the PHP app so existing desktop clients
// need no changes to work with oCIS.
package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/auth"
	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/blockmap"
	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/storage"
)

// Handler holds shared state for all API endpoints.
type Handler struct {
	store   *storage.Client
	tempDir string

	// In-memory block-map cache: key = "username:path:etag"
	cacheMu sync.RWMutex
	cache   map[string][]byte // JSON-encoded blockmap.Map
}

// New returns a Handler wired to the given oCIS storage client.
// tempDir is where in-flight block data is buffered before finalize.
func New(store *storage.Client, tempDir string) *Handler {
	return &Handler{
		store:   store,
		tempDir: tempDir,
		cache:   make(map[string][]byte),
	}
}

// -------------------------------------------------------------------------
// GET /api/status
// -------------------------------------------------------------------------

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"app":       "crispcloud_delta",
		"version":   "0.1.0",
		"status":    "ok",
		"blockSize": blockmap.DefaultBlockSize,
		"algorithm": "adler32+sha256",
	})
}

// -------------------------------------------------------------------------
// GET /api/blockmap/{path...}
// -------------------------------------------------------------------------

func (h *Handler) GetBlockMap(w http.ResponseWriter, r *http.Request) {
	authHdr, username, filePath, ok := extractContext(w, r)
	if !ok {
		return
	}

	// HEAD the file to get size + ETag without a full download.
	size, etag, err := h.store.StatFile(authHdr, filePath)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			respondError(w, http.StatusNotFound, "file not found")
		} else {
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Serve from cache when ETag matches.
	cacheKey := username + ":" + filePath + ":" + etag
	h.cacheMu.RLock()
	cached, hit := h.cache[cacheKey]
	h.cacheMu.RUnlock()
	if hit {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(cached) //nolint:errcheck
		return
	}

	// Stream GET → compute block map on the fly (avoids holding 500 MB in RAM twice).
	var buf bytes.Buffer
	buf.Grow(int(size))
	if _, _, err := h.store.StreamGet(authHdr, filePath, &buf); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("download: %v", err))
		return
	}

	bm, err := blockmap.Compute(bytes.NewReader(buf.Bytes()), "/"+filePath, size, etag)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("compute: %v", err))
		return
	}

	encoded, err := json.Marshal(bm)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "marshal")
		return
	}

	h.cacheMu.Lock()
	h.cache[cacheKey] = encoded
	h.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(encoded) //nolint:errcheck
}

// -------------------------------------------------------------------------
// POST /api/blocks/{path...}?offset=N&size=M
// -------------------------------------------------------------------------
// Block data is buffered locally. The full patched file is sent to oCIS only
// at finalize time — this avoids a GET+PUT cycle per block for large files.

func (h *Handler) PutBlock(w http.ResponseWriter, r *http.Request) {
	_, username, filePath, ok := extractContext(w, r)
	if !ok {
		return
	}

	offsetStr := r.URL.Query().Get("offset")
	sizeStr := r.URL.Query().Get("size")
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		respondError(w, http.StatusBadRequest, "invalid offset")
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, blockmap.DefaultBlockSize*2))
	if err != nil {
		respondError(w, http.StatusBadRequest, "reading body")
		return
	}
	if len(data) == 0 {
		respondError(w, http.StatusBadRequest, "empty body")
		return
	}
	if sizeStr != "" {
		if declared, _ := strconv.ParseInt(sizeStr, 10, 64); declared > 0 && int64(len(data)) != declared {
			respondError(w, http.StatusBadRequest,
				fmt.Sprintf("size mismatch: declared %d, received %d", declared, len(data)))
			return
		}
	}

	if err := h.writeBlockTemp(username, filePath, offset, data); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("buffer block: %v", err))
		return
	}

	log.Printf("buffered block offset=%d size=%d for %s/%s", offset, len(data), username, filePath)
	respondJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "offset": offset, "size": len(data),
	})
}

// -------------------------------------------------------------------------
// POST /api/finalize/{path...}?size=N
// -------------------------------------------------------------------------

func (h *Handler) Finalize(w http.ResponseWriter, r *http.Request) {
	authHdr, username, filePath, ok := extractContext(w, r)
	if !ok {
		return
	}

	newSize := int64(-1)
	if s := r.URL.Query().Get("size"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			newSize = v
		}
	}

	// Read buffered blocks (may be empty if no blocks changed).
	blocks, err := h.readBlocksTemp(username, filePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("read temp blocks: %v", err))
		return
	}

	// Download current file content from oCIS.
	content, size, etag, err := h.store.GetFile(authHdr, filePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("download: %v", err))
		return
	}

	// Grow the buffer if any block lands past the current EOF.
	needed := size
	for off, data := range blocks {
		if end := off + int64(len(data)); end > needed {
			needed = end
		}
	}
	if needed > size {
		content = append(content, make([]byte, needed-size)...)
	}

	// Apply each buffered block at its offset.
	for off, data := range blocks {
		copy(content[off:], data)
	}

	// Truncate if the new size is smaller than the patched content.
	if newSize >= 0 && newSize < int64(len(content)) {
		content = content[:newSize]
	}

	// Upload the patched file back to oCIS.
	if err := h.store.PutFile(authHdr, filePath, content); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("upload: %v", err))
		return
	}

	// Clean up temp blocks.
	h.clearBlocksTemp(username, filePath)

	// Recompute block map and update cache.
	bm, err := blockmap.Compute(bytes.NewReader(content), "/"+filePath, int64(len(content)), etag)
	if err == nil {
		if encoded, err := json.Marshal(bm); err == nil {
			h.cacheMu.Lock()
			// Invalidate old ETag entries and store fresh map under an approximate key.
			// The next getBlockMap will replace it with the real post-upload ETag.
			h.cache[username+":"+filePath+":"+etag] = encoded
			h.cacheMu.Unlock()
		}
	}

	log.Printf("finalized %s/%s (%d bytes)", username, filePath, len(content))
	respondJSON(w, http.StatusOK, map[string]any{"status": "finalized"})
}

// -------------------------------------------------------------------------
// Temp-file helpers — one file per block, named by offset
// -------------------------------------------------------------------------

func (h *Handler) blockTempDir(username, filePath string) string {
	// Encode the path so slashes don't create subdirectories.
	pathHash := sha256hex(filePath)[:16]
	userHash := sha256hex(username)[:16]
	return filepath.Join(h.tempDir, userHash, pathHash)
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) writeBlockTemp(username, filePath string, offset int64, data []byte) error {
	dir := h.blockTempDir(username, filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := filepath.Join(dir, strconv.FormatInt(offset, 10))
	return os.WriteFile(name, data, 0o600)
}

func (h *Handler) readBlocksTemp(username, filePath string) (map[int64][]byte, error) {
	dir := h.blockTempDir(username, filePath)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	blocks := make(map[int64][]byte, len(entries))
	for _, e := range entries {
		offset, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue // skip unexpected files
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		blocks[offset] = data
	}
	return blocks, nil
}

func (h *Handler) clearBlocksTemp(username, filePath string) {
	os.RemoveAll(h.blockTempDir(username, filePath))
}

// -------------------------------------------------------------------------
// Shared helpers
// -------------------------------------------------------------------------

// extractContext validates auth, extracts username, and parses the file path.
// Returns false (and writes an error response) on failure.
func extractContext(w http.ResponseWriter, r *http.Request) (authHdr, username, filePath string, ok bool) {
	authHdr = r.Header.Get("Authorization")
	if authHdr == "" {
		respondError(w, http.StatusUnauthorized, "missing Authorization header")
		return
	}
	var err error
	username, err = auth.UsernameFromHeader(authHdr)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "cannot parse token: "+err.Error())
		return
	}
	// In Go 1.22 ServeMux, {path...} captures everything after the prefix.
	filePath = strings.TrimLeft(r.PathValue("path"), "/")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "missing file path")
		return
	}
	ok = true
	return
}

func respondJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func respondError(w http.ResponseWriter, code int, msg string) {
	respondJSON(w, code, map[string]string{"error": msg})
}
