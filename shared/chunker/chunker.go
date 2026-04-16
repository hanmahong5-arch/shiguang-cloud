// Package chunker implements content-addressable 4MB chunk splitting and
// reassembly for the game client patcher.
//
// Design rationale (Gemini review Issue #3):
//   - 4MB chunks (not 64KB): 10GB client = ~2500 chunks vs 163840 chunks
//   - Manifest stays under 200KB vs 8MB
//   - Worst-case overhead: 4MB for a 1-byte change, but zstd compresses
//     to ~500KB which is acceptable for game patching
//
// Each chunk is identified by its SHA-256 hash, enabling deduplication
// and integrity verification. The manifest records: file path, chunk
// index, offset, size, and hash.
package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ChunkSize is 4MB — the fixed block size for all chunking.
const ChunkSize = 4 * 1024 * 1024

// FileChunk represents one chunk of a file in the manifest.
type FileChunk struct {
	Path   string `json:"path"`   // relative file path within the client
	Index  int    `json:"index"`  // chunk index within the file (0-based)
	Offset int64  `json:"offset"` // byte offset within the file
	Size   int    `json:"size"`   // actual bytes in this chunk (<= ChunkSize)
	Hash   string `json:"hash"`   // SHA-256 hex of the chunk data
}

// Manifest is the full chunk index for a game client directory.
type Manifest struct {
	Version   string      `json:"version"`             // manifest version for forward-compat
	Root      string      `json:"root"`                // base directory (relative)
	Chunks    []FileChunk `json:"chunks"`
	Signature string      `json:"signature,omitempty"` // HMAC-SHA256 hex (set by signed export)
}

// BuildManifest scans a directory tree and computes 4MB chunks + SHA-256
// for every file. Returns the manifest without writing any chunk data.
func BuildManifest(root string) (*Manifest, error) {
	m := &Manifest{
		Version: "1",
		Root:    filepath.Base(root),
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Normalize to forward slashes for cross-platform manifest
		relPath = strings.ReplaceAll(relPath, "\\", "/")

		chunks, err := chunkFile(path, relPath)
		if err != nil {
			return fmt.Errorf("chunk %s: %w", relPath, err)
		}
		m.Chunks = append(m.Chunks, chunks...)
		return nil
	})

	return m, err
}

// chunkFile splits one file into 4MB chunks and computes SHA-256 for each.
func chunkFile(absPath, relPath string) ([]FileChunk, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []FileChunk
	buf := make([]byte, ChunkSize)
	var offset int64
	index := 0

	for {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			h := sha256.Sum256(buf[:n])
			chunks = append(chunks, FileChunk{
				Path:   relPath,
				Index:  index,
				Offset: offset,
				Size:   n,
				Hash:   hex.EncodeToString(h[:]),
			})
			offset += int64(n)
			index++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return chunks, nil
}

// ExportStats reports the result of an ExportChunks operation.
type ExportStats struct {
	TotalChunks int   // total chunks in manifest
	NewChunks   int   // newly written chunks
	SkippedDup  int   // skipped (already exist on disk)
	TotalBytes  int64 // total raw bytes across all chunks
}

// ExportChunks scans the client directory, builds a manifest, and writes
// each chunk to outDir/chunks/{sha256_hash}. Content-addressable storage
// means identical chunks are automatically deduplicated — if a chunk file
// already exists, it is skipped. The manifest is written to
// outDir/chunk-manifest.json.
//
// Directory layout after export:
//
//	outDir/
//	  chunk-manifest.json
//	  chunks/
//	    {sha256_hex_1}
//	    {sha256_hex_2}
//	    ...
func ExportChunks(clientRoot, outDir string, hmacKey ...string) (*ExportStats, error) {
	// Phase 1: build manifest (scan + hash)
	manifest, err := BuildManifest(clientRoot)
	if err != nil {
		return nil, fmt.Errorf("build manifest: %w", err)
	}

	chunksDir := filepath.Join(outDir, "chunks")
	if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create chunks dir: %w", err)
	}

	stats := &ExportStats{TotalChunks: len(manifest.Chunks)}

	// Phase 2: export each chunk to content-addressable store
	// Build a set of unique hashes to avoid re-reading the same file
	// for duplicate chunks (common in multi-version clients).
	exported := make(map[string]bool, len(manifest.Chunks))

	for _, c := range manifest.Chunks {
		stats.TotalBytes += int64(c.Size)

		if exported[c.Hash] {
			stats.SkippedDup++
			continue
		}
		exported[c.Hash] = true

		chunkPath := filepath.Join(chunksDir, c.Hash)

		// Content-addressable: if file exists, skip (dedup across builds)
		if fi, err := os.Stat(chunkPath); err == nil && fi.Size() == int64(c.Size) {
			stats.SkippedDup++
			continue
		}

		// Read chunk data from source file
		srcPath := filepath.Join(clientRoot, filepath.FromSlash(c.Path))
		data, err := readChunkData(srcPath, c.Offset, c.Size)
		if err != nil {
			return nil, fmt.Errorf("read chunk %s[%d]: %w", c.Path, c.Index, err)
		}

		// Verify hash before writing (defense against filesystem corruption)
		if !VerifyChunk(data, c.Hash) {
			return nil, fmt.Errorf("hash mismatch reading %s chunk %d (source changed during export?)", c.Path, c.Index)
		}

		// Atomic write: temp file + rename to prevent partial chunks on crash
		if err := atomicWrite(chunkPath, data); err != nil {
			return nil, fmt.Errorf("write chunk %s: %w", c.Hash[:12], err)
		}

		stats.NewChunks++
		if stats.NewChunks%500 == 0 {
			log.Printf("[chunker] exported %d/%d chunks...", stats.NewChunks, stats.TotalChunks)
		}
	}

	// Phase 3: write manifest (with optional HMAC signature)
	manifestPath := filepath.Join(outDir, "chunk-manifest.json")
	var key string
	if len(hmacKey) > 0 {
		key = hmacKey[0]
	}
	if err := WriteManifest(manifestPath, manifest, key); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	return stats, nil
}

// readChunkData reads exactly `size` bytes from `path` starting at `offset`.
func readChunkData(path string, offset int64, size int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// atomicWrite writes data to a temp file then renames to dst.
// Prevents partial/corrupt chunks if the process is interrupted.
func atomicWrite(dst string, data []byte) error {
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// VerifyChunk checks that a chunk's data matches its expected SHA-256 hash.
func VerifyChunk(data []byte, expectedHash string) bool {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) == expectedHash
}
