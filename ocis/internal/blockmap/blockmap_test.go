package blockmap_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/CrispStrobe/crispcloud-delta-sync/ocis/internal/blockmap"
)

// RFC 1950 known value — must match the PHP BlockMapService and C++ adler32().
func TestChecksumWikipedia(t *testing.T) {
	got := blockmap.Checksum([]byte("Wikipedia"))
	want := uint32(0x11E60398)
	if got != want {
		t.Fatalf("Checksum(Wikipedia) = 0x%08X, want 0x%08X", got, want)
	}
}

func TestChecksumEmpty(t *testing.T) {
	if got := blockmap.Checksum(nil); got != 1 {
		t.Fatalf("Checksum(nil) = %d, want 1", got)
	}
}

func TestChecksumHello(t *testing.T) {
	got := blockmap.Checksum([]byte("hello"))
	want := uint32(0x062C0215)
	if got != want {
		t.Fatalf("Checksum(hello) = 0x%08X, want 0x%08X", got, want)
	}
}

func TestComputeSingleBlock(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 1024*1024) // 1 MB — fits in one 4 MB block
	m, err := blockmap.Compute(bytes.NewReader(data), "/test.bin", int64(len(data)), "etag1")
	if err != nil {
		t.Fatal(err)
	}
	if m.BlockCount != 1 {
		t.Fatalf("BlockCount = %d, want 1", m.BlockCount)
	}
	if m.TotalSize != int64(len(data)) {
		t.Fatalf("TotalSize = %d, want %d", m.TotalSize, len(data))
	}
	if len(m.Signatures) != 1 {
		t.Fatalf("len(Signatures) = %d, want 1", len(m.Signatures))
	}
	sig := m.Signatures[0]
	if sig.WeakHash != blockmap.Checksum(data) {
		t.Errorf("WeakHash mismatch")
	}
	sum := sha256.Sum256(data)
	if sig.StrongHash != hex.EncodeToString(sum[:]) {
		t.Errorf("StrongHash mismatch")
	}
}

func TestComputeMultipleBlocks(t *testing.T) {
	// 10 MB = 2 full 4 MB blocks + 1 partial block
	size := 10 * 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	blockSize := int64(blockmap.DefaultBlockSize)
	m, err := blockmap.Compute(bytes.NewReader(data), "/big.bin", int64(size), "etag2")
	if err != nil {
		t.Fatal(err)
	}
	if m.BlockCount != 3 {
		t.Fatalf("BlockCount = %d, want 3", m.BlockCount)
	}
	if m.Signatures[0].Offset != 0 || m.Signatures[0].Size != blockSize {
		t.Errorf("block 0: offset=%d size=%d", m.Signatures[0].Offset, m.Signatures[0].Size)
	}
	if m.Signatures[1].Offset != blockSize || m.Signatures[1].Size != blockSize {
		t.Errorf("block 1: offset=%d size=%d", m.Signatures[1].Offset, m.Signatures[1].Size)
	}
	wantTail := int64(size) - 2*blockSize
	if m.Signatures[2].Offset != 2*blockSize || m.Signatures[2].Size != wantTail {
		t.Errorf("block 2: offset=%d size=%d, want tail=%d", m.Signatures[2].Offset, m.Signatures[2].Size, wantTail)
	}
	// Verify block 0 hash
	block0 := data[:blockSize]
	if m.Signatures[0].WeakHash != blockmap.Checksum(block0) {
		t.Errorf("block 0 WeakHash mismatch")
	}
}

func TestComputeEmptyFile(t *testing.T) {
	m, err := blockmap.Compute(bytes.NewReader(nil), "/empty.bin", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.BlockCount != 0 || len(m.Signatures) != 0 {
		t.Fatalf("empty file should have 0 blocks, got %d", m.BlockCount)
	}
}

func TestETagPassthrough(t *testing.T) {
	m, err := blockmap.Compute(bytes.NewReader([]byte("x")), "/f", 1, "my-etag-42")
	if err != nil {
		t.Fatal(err)
	}
	if m.ETag != "my-etag-42" {
		t.Fatalf("ETag = %q, want my-etag-42", m.ETag)
	}
}
