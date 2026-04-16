package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestChunkFile(t *testing.T) {
	dir := t.TempDir()

	// Create a test file: 4MB + 100 bytes = 2 chunks
	path := filepath.Join(dir, "test.bin")
	data := make([]byte, ChunkSize+100)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, err := chunkFile(path, "test.bin")
	if err != nil {
		t.Fatal(err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Chunk 0: full 4MB
	if chunks[0].Size != ChunkSize {
		t.Errorf("chunk 0 size = %d, want %d", chunks[0].Size, ChunkSize)
	}
	if chunks[0].Offset != 0 {
		t.Errorf("chunk 0 offset = %d, want 0", chunks[0].Offset)
	}

	// Chunk 1: remaining 100 bytes
	if chunks[1].Size != 100 {
		t.Errorf("chunk 1 size = %d, want 100", chunks[1].Size)
	}
	if chunks[1].Offset != int64(ChunkSize) {
		t.Errorf("chunk 1 offset = %d, want %d", chunks[1].Offset, ChunkSize)
	}

	// Verify hashes are correct
	h0 := sha256.Sum256(data[:ChunkSize])
	if chunks[0].Hash != hex.EncodeToString(h0[:]) {
		t.Error("chunk 0 hash mismatch")
	}

	h1 := sha256.Sum256(data[ChunkSize:])
	if chunks[1].Hash != hex.EncodeToString(h1[:]) {
		t.Error("chunk 1 hash mismatch")
	}
}

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()

	// Create a file structure
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(sub, "b.txt"), []byte("world"), 0o644)

	m, err := BuildManifest(dir)
	if err != nil {
		t.Fatal(err)
	}

	if m.Version != "1" {
		t.Errorf("version = %q, want 1", m.Version)
	}

	if len(m.Chunks) != 2 {
		t.Fatalf("expected 2 chunks (one per small file), got %d", len(m.Chunks))
	}

	// Both files are <4MB, so each has exactly 1 chunk
	paths := map[string]bool{}
	for _, c := range m.Chunks {
		paths[c.Path] = true
		if c.Index != 0 {
			t.Errorf("small file %s should have index 0, got %d", c.Path, c.Index)
		}
	}

	if !paths["a.txt"] || !paths["sub/b.txt"] {
		t.Errorf("expected a.txt and sub/b.txt, got %v", paths)
	}
}

func TestVerifyChunk(t *testing.T) {
	data := []byte("test chunk data")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	if !VerifyChunk(data, hash) {
		t.Error("VerifyChunk returned false for correct hash")
	}

	if VerifyChunk(data, "wrong") {
		t.Error("VerifyChunk returned true for wrong hash")
	}
}

func TestManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{
		Version: "1",
		Root:    "client",
		Chunks: []FileChunk{
			{Path: "a.bin", Index: 0, Offset: 0, Size: 100, Hash: "abc123"},
			{Path: "a.bin", Index: 1, Offset: 100, Size: 50, Hash: "def456"},
		},
	}

	if err := WriteManifest(path, m); err != nil {
		t.Fatal(err)
	}

	m2, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}

	if m2.Version != "1" || m2.Root != "client" || len(m2.Chunks) != 2 {
		t.Errorf("roundtrip mismatch: %+v", m2)
	}

	if m2.Chunks[0].Hash != "abc123" || m2.Chunks[1].Hash != "def456" {
		t.Error("hash roundtrip mismatch")
	}
}

func TestExportChunks(t *testing.T) {
	// Setup: create a source directory with two files
	srcDir := t.TempDir()
	outDir := t.TempDir()

	// File 1: exactly 4MB + 10 bytes → 2 chunks
	bigData := make([]byte, ChunkSize+10)
	for i := range bigData {
		bigData[i] = byte(i % 251) // prime modulus for variety
	}
	os.WriteFile(filepath.Join(srcDir, "big.bin"), bigData, 0o644)

	// File 2: small file → 1 chunk
	os.WriteFile(filepath.Join(srcDir, "small.txt"), []byte("hello world"), 0o644)

	// Export
	stats, err := ExportChunks(srcDir, outDir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify stats
	if stats.TotalChunks != 3 {
		t.Errorf("TotalChunks = %d, want 3", stats.TotalChunks)
	}
	if stats.NewChunks != 3 {
		t.Errorf("NewChunks = %d, want 3", stats.NewChunks)
	}
	if stats.SkippedDup != 0 {
		t.Errorf("SkippedDup = %d, want 0", stats.SkippedDup)
	}

	// Verify manifest file exists
	manifestPath := filepath.Join(outDir, "chunk-manifest.json")
	m, err := ReadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Chunks) != 3 {
		t.Fatalf("manifest has %d chunks, want 3", len(m.Chunks))
	}

	// Verify each chunk file exists and has correct content
	chunksDir := filepath.Join(outDir, "chunks")
	for _, c := range m.Chunks {
		chunkPath := filepath.Join(chunksDir, c.Hash)
		data, err := os.ReadFile(chunkPath)
		if err != nil {
			t.Fatalf("chunk file %s missing: %v", c.Hash[:12], err)
		}
		if len(data) != c.Size {
			t.Errorf("chunk %s size = %d, want %d", c.Hash[:12], len(data), c.Size)
		}
		if !VerifyChunk(data, c.Hash) {
			t.Errorf("chunk %s hash mismatch", c.Hash[:12])
		}
	}

	// Re-export: should skip all chunks (dedup)
	stats2, err := ExportChunks(srcDir, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.NewChunks != 0 {
		t.Errorf("second export: NewChunks = %d, want 0 (all dedup)", stats2.NewChunks)
	}
	if stats2.SkippedDup != 3 {
		t.Errorf("second export: SkippedDup = %d, want 3", stats2.SkippedDup)
	}
}

func TestExportChunksAtomicWrite(t *testing.T) {
	// Test that partial temp files don't leak if export succeeds
	srcDir := t.TempDir()
	outDir := t.TempDir()

	os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("data"), 0o644)

	if _, err := ExportChunks(srcDir, outDir); err != nil {
		t.Fatal(err)
	}

	// No .tmp files should remain
	entries, _ := os.ReadDir(filepath.Join(outDir, "chunks"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestManifestHMACSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signed.json")
	key := "test-secret-key-2024"

	m := &Manifest{
		Version: "1",
		Root:    "client",
		Chunks: []FileChunk{
			{Path: "a.bin", Index: 0, Offset: 0, Size: 100, Hash: "abc123"},
			{Path: "b.bin", Index: 0, Offset: 0, Size: 200, Hash: "def456"},
		},
	}

	// Write with signing
	if err := WriteManifest(path, m, key); err != nil {
		t.Fatal(err)
	}

	// Read back — should have a non-empty signature
	m2, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Signature == "" {
		t.Fatal("expected non-empty signature after signed write")
	}

	// Verify with correct key → should pass
	if err := VerifyManifestSignature(m2, key); err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}

	// Verify with wrong key → should fail
	if err := VerifyManifestSignature(m2, "wrong-key"); err == nil {
		t.Error("expected signature mismatch with wrong key")
	}

	// Tamper with a chunk hash → should fail
	m2.Chunks[0].Hash = "tampered"
	if err := VerifyManifestSignature(m2, key); err == nil {
		t.Error("expected signature mismatch after hash tamper")
	}

	// Unsigned manifest with key → should pass (backwards compatible)
	unsigned := &Manifest{Version: "1", Chunks: []FileChunk{{Hash: "x"}}}
	if err := VerifyManifestSignature(unsigned, key); err != nil {
		t.Errorf("unsigned manifest should pass verification, got: %v", err)
	}

	// Signed manifest without key → should pass (skip verification)
	if err := VerifyManifestSignature(m, ""); err != nil {
		t.Errorf("empty key should skip verification, got: %v", err)
	}
}

func TestDiffManifests(t *testing.T) {
	old := &Manifest{Chunks: []FileChunk{
		{Path: "a", Hash: "aaa"},
		{Path: "b", Hash: "bbb"},
	}}
	cur := &Manifest{Chunks: []FileChunk{
		{Path: "a", Hash: "aaa"}, // same
		{Path: "b", Hash: "bbb2"}, // changed
		{Path: "c", Hash: "ccc"}, // new
	}}

	diff := DiffManifests(old, cur)
	if len(diff) != 2 {
		t.Fatalf("expected 2 diff chunks, got %d", len(diff))
	}

	hashes := map[string]bool{}
	for _, c := range diff {
		hashes[c.Hash] = true
	}
	if !hashes["bbb2"] || !hashes["ccc"] {
		t.Errorf("expected bbb2 and ccc in diff, got %v", hashes)
	}
}
