// crispcloud-delta — oCIS extension for block-level delta sync.
//
// Exposes the same REST API as the PHP crispcloud_delta Nextcloud/ownCloud app
// so existing desktop clients (nextcloud-desktop, owncloud-client) work
// against oCIS without modification.
//
// Configuration (environment variables):
//
//	OCIS_URL     Base URL of the oCIS instance  (default: http://localhost:9200)
//	LISTEN_ADDR  TCP address to listen on       (default: :8090)
//	TEMP_DIR     Directory for in-flight blocks  (default: /tmp/crispcloud_delta)
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/handler"
	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/storage"
)

func main() {
	ocisURL := getenv("OCIS_URL", "http://localhost:9200")
	listenAddr := getenv("LISTEN_ADDR", ":8090")
	tempDir := getenv("TEMP_DIR", "/tmp/crispcloud_delta")

	store := storage.New(ocisURL)
	h := handler.New(store, tempDir)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", h.Status)
	mux.HandleFunc("GET /api/blockmap/{path...}", h.GetBlockMap)
	mux.HandleFunc("POST /api/blocks/{path...}", h.PutBlock)
	mux.HandleFunc("POST /api/finalize/{path...}", h.Finalize)

	log.Printf("crispcloud-delta oCIS extension → %s  (proxying to %s)", listenAddr, ocisURL)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
