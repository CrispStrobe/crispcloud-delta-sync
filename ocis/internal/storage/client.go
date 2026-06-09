// Package storage provides a minimal WebDAV client for oCIS file operations.
// All requests pass the caller's Authorization header through unchanged so
// oCIS handles token validation independently.
package storage

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client makes WebDAV calls to an oCIS instance.
type Client struct {
	ocisURL string
	http    *http.Client
}

// New returns a Client for the given oCIS base URL (e.g. "https://cloud.example.com").
func New(ocisURL string) *Client {
	return &Client{
		ocisURL: strings.TrimRight(ocisURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// webdavURL builds the legacy-compat WebDAV URL for a user-relative path.
// oCIS proxies /remote.php/webdav/ to the authenticated user's personal space.
func (c *Client) webdavURL(path string) string {
	return c.ocisURL + "/remote.php/webdav/" + strings.TrimLeft(path, "/")
}

// GetFile downloads path and returns its full content, Content-Length, and ETag.
func (c *Client) GetFile(authHeader, path string) ([]byte, int64, string, error) {
	req, err := http.NewRequest(http.MethodGet, c.webdavURL(path), nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, 0, "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, "", fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, "", fmt.Errorf("reading body: %w", err)
	}
	etag := stripQuotes(resp.Header.Get("ETag"))
	return body, int64(len(body)), etag, nil
}

// StatFile returns size and ETag without downloading the body.
func (c *Client) StatFile(authHeader, path string) (int64, string, error) {
	req, err := http.NewRequest(http.MethodHead, c.webdavURL(path), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("HEAD %s: HTTP %d", path, resp.StatusCode)
	}
	etag := stripQuotes(resp.Header.Get("ETag"))
	return resp.ContentLength, etag, nil
}

// StreamGet calls GET and writes the body to w, returning size and ETag.
// Used for block-map computation to avoid holding the full file in memory.
func (c *Client) StreamGet(authHeader, path string, w io.Writer) (int64, string, error) {
	req, err := http.NewRequest(http.MethodGet, c.webdavURL(path), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	etag := stripQuotes(resp.Header.Get("ETag"))
	n, err := io.Copy(w, resp.Body)
	return n, etag, err
}

// PutFile uploads data to path via WebDAV PUT.
func (c *Client) PutFile(authHeader, path string, data []byte) error {
	req, err := http.NewRequest(http.MethodPut, c.webdavURL(path), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("PUT %s: HTTP %d: %s", path, resp.StatusCode, string(body))
}

// MkDir creates a WebDAV collection (MKCOL). Ignores 405 (already exists).
func (c *Client) MkDir(authHeader, path string) error {
	req, err := http.NewRequest("MKCOL", c.webdavURL(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusMethodNotAllowed: // 405 = already exists
		return nil
	}
	return fmt.Errorf("MKCOL %s: HTTP %d", path, resp.StatusCode)
}

// ErrNotFound is returned when oCIS responds with 404.
var ErrNotFound = fmt.Errorf("not found")

func stripQuotes(s string) string { return strings.Trim(s, `"`) }
