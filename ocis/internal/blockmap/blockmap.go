// Package blockmap computes and serialises Adler-32 + SHA-256 block maps.
// The algorithm is identical to the PHP BlockMapService and the C++
// PropagateUploadFileDelta implementations so hashes are directly comparable.
package blockmap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/adler32"
	"io"
	"math"
	"time"
)

// DefaultBlockSize matches the server PHP app and both desktop clients.
const DefaultBlockSize = 4 * 1024 * 1024

// Signature holds the weak and strong hash for one block.
type Signature struct {
	BlockIndex int    `json:"blockIndex"`
	Offset     int64  `json:"offset"`
	Size       int64  `json:"size"`
	WeakHash   uint32 `json:"weakHash"`
	StrongHash string `json:"strongHash"`
}

// Map is the block-map JSON structure returned to clients.
type Map struct {
	FilePath   string      `json:"filePath"`
	TotalSize  int64       `json:"totalSize"`
	BlockSize  int64       `json:"blockSize"`
	BlockCount int         `json:"blockCount"`
	Signatures []Signature `json:"signatures"`
	CreatedAt  string      `json:"createdAt"`
	ETag       string      `json:"etag,omitempty"`
}

// Checksum returns the RFC 1950 Adler-32 checksum.
// Uses Go's stdlib hash/adler32 which implements the same algorithm as the PHP
// adler32() in BlockMapService and the C++ adler32() in propagateuploaddelta.cpp.
func Checksum(data []byte) uint32 {
	return adler32.Checksum(data)
}

// Compute reads r (exactly totalSize bytes) and returns the block map.
func Compute(r io.Reader, filePath string, totalSize int64, etag string) (*Map, error) {
	blockSize := int64(DefaultBlockSize)
	blockCount := 0
	if totalSize > 0 {
		blockCount = int(math.Ceil(float64(totalSize) / float64(blockSize)))
	}

	sigs := make([]Signature, 0, blockCount)
	for i := 0; i < blockCount; i++ {
		offset := int64(i) * blockSize
		size := blockSize
		if offset+size > totalSize {
			size = totalSize - offset
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("block %d at offset %d: %w", i, offset, err)
		}
		sum := sha256.Sum256(data)
		sigs = append(sigs, Signature{
			BlockIndex: i,
			Offset:     offset,
			Size:       size,
			WeakHash:   Checksum(data),
			StrongHash: hex.EncodeToString(sum[:]),
		})
	}

	return &Map{
		FilePath:   filePath,
		TotalSize:  totalSize,
		BlockSize:  blockSize,
		BlockCount: blockCount,
		Signatures: sigs,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ETag:       etag,
	}, nil
}
