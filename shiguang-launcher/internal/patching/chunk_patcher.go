// chunk_patcher.go extends the Patcher with chunk-aware parallel downloads
// using the shared/chunker library.
//
// Pipeline:
//  1. Fetch remote chunk manifest from patch server
//  2. Build local manifest of current client state (SHA-256 per 4MB block)
//  3. DiffManifests to identify changed chunks
//  4. Download changed chunks in parallel (8 workers)
//  5. Write each chunk to the correct file offset
//  6. Verify SHA-256 after write
//
// Falls back to legacy file-level patching if the remote chunk manifest
// is unavailable (HTTP 404).
package patching

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiguang/shared/chunker"
)

const (
	chunkWorkers   = 8
	chunkMaxRetry  = 3
	chunkRetryBase = 500 * time.Millisecond
)

// safeChunkPath rejects manifest-supplied relative paths that would escape
// the client root. Rules:
//   - Must not be empty.
//   - Must not be absolute (filepath.IsAbs catches "/x" and "C:\\x").
//   - Must not contain ".." segments after normalisation.
//   - Must not contain NUL bytes (Windows/NTFS may silently truncate).
//
// Accepts both forward and backslash separators so cross-platform manifests
// (built on Linux, applied on Windows) still work.
func safeChunkPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("contains NUL byte")
	}
	// Normalise separators then use filepath.Clean to collapse ".." / ".".
	norm := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(norm) {
		return fmt.Errorf("absolute path")
	}
	// After Clean, any surviving ".." must be a prefix — means it escapes root.
	if norm == ".." || strings.HasPrefix(norm, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes root via '..'")
	}
	// Windows drive-relative shorthand like "C:foo" — Clean doesn't flag it.
	if len(norm) >= 2 && norm[1] == ':' {
		return fmt.Errorf("contains drive letter")
	}
	return nil
}

// RunChunked attempts chunk-based patching. Returns ErrNoChunkManifest if
// the remote server doesn't publish a chunk manifest, allowing the caller
// to fall back to legacy file-level patching.
func (p *Patcher) RunChunked(ctx context.Context) error {
	// Phase 1: fetch remote chunk manifest
	p.progress("fetching_manifest", 0, 0, "")
	remoteManifest, err := p.fetchChunkManifest(ctx)
	if err != nil {
		return fmt.Errorf("fetch chunk manifest: %w", err)
	}

	// Verify manifest HMAC signature if a key is configured
	if err := chunker.VerifyManifestSignature(remoteManifest, p.hmacKey); err != nil {
		return fmt.Errorf("manifest integrity: %w", err)
	}

	// Phase 2: build local manifest
	p.progress("scanning_local", 0, 0, "")
	localManifest, err := chunker.BuildManifest(p.clientRoot)
	if err != nil {
		return fmt.Errorf("build local manifest: %w", err)
	}

	// Phase 3: compute diff
	p.progress("computing_diff", 0, 0, "")
	diff := chunker.DiffManifests(localManifest, remoteManifest)
	if len(diff) == 0 {
		p.progress("up_to_date", 0, 0, "")
		return nil
	}

	// Compute total download bytes
	var totalBytes int64
	for _, c := range diff {
		totalBytes += int64(c.Size)
	}
	p.progress("downloading_chunks", 0, totalBytes,
		fmt.Sprintf("%d chunks to download", len(diff)))

	// Phase 4: parallel download + write
	if err := p.downloadChunksParallel(ctx, diff, totalBytes); err != nil {
		return err
	}

	// Phase 5: verify all written chunks
	p.progress("verifying_chunks", 0, int64(len(diff)), "")
	if err := p.verifyWrittenChunks(diff); err != nil {
		return err
	}

	p.progress("complete", totalBytes, totalBytes, "")
	return nil
}

// fetchChunkManifest downloads the chunk manifest from the patch server.
// Expects the manifest at the same base path as the legacy manifest,
// with filename "chunk-manifest.json".
func (p *Patcher) fetchChunkManifest(ctx context.Context) (*chunker.Manifest, error) {
	// Derive chunk manifest URL from the legacy manifest URL
	// e.g. https://patch.example.com/manifest.json -> https://patch.example.com/chunk-manifest.json
	url := p.manifestURL
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		url = url[:idx+1] + "chunk-manifest.json"
	} else {
		url = "chunk-manifest.json"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("chunk manifest not found at %s (404)", url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching chunk manifest", resp.StatusCode)
	}

	return chunker.DecodeManifest(resp.Body)
}

// downloadChunksParallel downloads chunks using a worker pool and writes
// each chunk to the correct file offset. Thread-safe via per-file mutexes.
func (p *Patcher) downloadChunksParallel(ctx context.Context, chunks []chunker.FileChunk, totalBytes int64) error {
	var done atomic.Int64
	var errOnce sync.Once
	var firstErr error

	// Per-file mutex map to prevent concurrent writes to the same file
	fileMu := &sync.Map{}

	// Derive base URL for chunk downloads: {manifest_base}/chunks/{hash}
	chunkBaseURL := p.manifestURL
	if idx := strings.LastIndex(chunkBaseURL, "/"); idx >= 0 {
		chunkBaseURL = chunkBaseURL[:idx+1] + "chunks/"
	}

	// Work queue
	work := make(chan chunker.FileChunk, len(chunks))
	for _, c := range chunks {
		work <- c
	}
	close(work)

	var wg sync.WaitGroup
	for w := 0; w < chunkWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				select {
				case <-ctx.Done():
					errOnce.Do(func() { firstErr = ctx.Err() })
					return
				default:
				}

				// Retry with exponential backoff + jitter
				var lastErr error
				for attempt := 0; attempt <= chunkMaxRetry; attempt++ {
					if attempt > 0 {
						backoff := chunkRetryBase * time.Duration(1<<(attempt-1))
						jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
						select {
						case <-ctx.Done():
							lastErr = ctx.Err()
							break
						case <-time.After(backoff + jitter):
						}
					}
					lastErr = p.downloadAndWriteChunk(ctx, chunkBaseURL, c, &done, totalBytes, fileMu)
					if lastErr == nil {
						break
					}
				}
				if lastErr != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("chunk %s[%d] (after %d retries): %w", c.Path, c.Index, chunkMaxRetry, lastErr)
					})
					return
				}
			}
		}()
	}

	wg.Wait()
	return firstErr
}

// downloadAndWriteChunk fetches one chunk from the server and writes it
// at the correct offset in the target file.
func (p *Patcher) downloadAndWriteChunk(
	ctx context.Context,
	chunkBaseURL string,
	c chunker.FileChunk,
	done *atomic.Int64,
	totalBytes int64,
	fileMu *sync.Map,
) error {
	// Download chunk by hash
	url := chunkBaseURL + c.Hash
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read chunk data into memory for verification
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.Size)+1024))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	// Verify SHA-256 before writing to disk
	if !chunker.VerifyChunk(data, c.Hash) {
		return fmt.Errorf("SHA-256 mismatch (download corrupted)")
	}

	// Write chunk at correct file offset (serialize per-file)
	// SECURITY (P0, path traversal): c.Path comes from a remote manifest the
	// launcher fetched over the network. A malicious patch server could embed
	// "..\\..\\windows\\system32\\x.dll" to escape clientRoot and overwrite
	// arbitrary files. We enforce two rules:
	//   1. Reject any path containing "..", absolute paths, or drive letters.
	//   2. After filepath.Join, verify the cleaned target is still under
	//      clientRoot (defence-in-depth against unicode / NTFS weirdness).
	if err := safeChunkPath(c.Path); err != nil {
		return fmt.Errorf("unsafe chunk path %q: %w", c.Path, err)
	}
	targetPath := filepath.Join(p.clientRoot, c.Path)
	rootAbs, _ := filepath.Abs(p.clientRoot)
	tgtAbs, _ := filepath.Abs(targetPath)
	if !strings.HasPrefix(tgtAbs+string(filepath.Separator), rootAbs+string(filepath.Separator)) && tgtAbs != rootAbs {
		return fmt.Errorf("chunk target %q escapes client root", c.Path)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	// Get or create per-file mutex
	mu, _ := fileMu.LoadOrStore(c.Path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(c.Offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Update progress
	newDone := done.Add(int64(c.Size))
	p.progress("downloading_chunks", newDone, totalBytes,
		fmt.Sprintf("%s (chunk %d)", c.Path, c.Index))

	return nil
}

// verifyWrittenChunks reads back all downloaded chunks and verifies SHA-256.
func (p *Patcher) verifyWrittenChunks(chunks []chunker.FileChunk) error {
	for i, c := range chunks {
		p.progress("verifying_chunks", int64(i), int64(len(chunks)), c.Path)

		// Re-apply the path traversal guard (defence in depth — the manifest
		// could have been tampered with between download and verify).
		if err := safeChunkPath(c.Path); err != nil {
			return fmt.Errorf("verify: unsafe path %q: %w", c.Path, err)
		}
		path := filepath.Join(p.clientRoot, c.Path)
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("verify %s[%d]: %w", c.Path, c.Index, err)
		}

		if _, err := f.Seek(c.Offset, io.SeekStart); err != nil {
			f.Close()
			return fmt.Errorf("verify seek %s[%d]: %w", c.Path, c.Index, err)
		}

		data := make([]byte, c.Size)
		if _, err := io.ReadFull(f, data); err != nil {
			f.Close()
			return fmt.Errorf("verify read %s[%d]: %w", c.Path, c.Index, err)
		}
		f.Close()

		if !chunker.VerifyChunk(data, c.Hash) {
			return fmt.Errorf("post-write SHA-256 mismatch: %s chunk %d", c.Path, c.Index)
		}
	}
	return nil
}
