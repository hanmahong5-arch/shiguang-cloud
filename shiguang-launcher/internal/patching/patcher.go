// Package patching downloads and verifies client patch files.
//
// Protocol: the control server publishes a JSON manifest at a stable URL.
// The launcher:
//  1. Downloads the manifest
//  2. For each file, compares the local MD5 against the manifest's expected MD5
//  3. Downloads mismatched files using HTTP Range (resume support)
//  4. Emits progress events via Wails runtime.EventsEmit
//
// Manifest format (JSON):
//
//	{
//	  "version": "1.0.5",
//	  "base_url": "https://example.com/patches/",
//	  "files": [
//	    {"path": "Data/animations.pak", "md5": "abc123...", "size": 12345678, "url": "data/animations.pak"}
//	  ]
//	}
package patching

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	legacyMaxRetry  = 3
	legacyRetryBase = 1 * time.Second
)

// Manifest is the top-level patch manifest.
type Manifest struct {
	Version string        `json:"version"`
	BaseURL string        `json:"base_url"`
	Files   []ManifestFile `json:"files"`
}

// ManifestFile describes one file the launcher should verify/download.
type ManifestFile struct {
	Path string `json:"path"` // relative to client root
	MD5  string `json:"md5"`  // expected 32-char hex
	Size int64  `json:"size"` // expected size in bytes
	URL  string `json:"url"`  // relative to Manifest.BaseURL
}

// ProgressFn is invoked periodically during patching. Total is 0 until the
// overall byte count has been computed.
type ProgressFn func(phase string, doneBytes, totalBytes int64, currentFile string)

// Patcher runs the verify + download pipeline.
type Patcher struct {
	clientRoot  string // on-disk directory being patched
	manifestURL string
	hmacKey     string // optional HMAC key for manifest signature verification
	client      *http.Client
	progress    ProgressFn
}

// NewPatcher constructs a patcher. If hmacKey is non-empty, the chunk
// manifest's HMAC-SHA256 signature is verified before trusting chunk hashes.
func NewPatcher(clientRoot, manifestURL string, progress ProgressFn, hmacKey ...string) *Patcher {
	if progress == nil {
		progress = func(string, int64, int64, string) {}
	}
	p := &Patcher{
		clientRoot:  clientRoot,
		manifestURL: manifestURL,
		client:      &http.Client{Timeout: 10 * time.Minute},
		progress:    progress,
	}
	if len(hmacKey) > 0 {
		p.hmacKey = hmacKey[0]
	}
	return p
}

// Run executes the full verify + download pipeline. Returns nil on success,
// or the first irrecoverable error encountered.
//
// Tries chunk-based patching first (parallel 8-worker SHA-256 verified).
// Falls back to legacy file-level patching if the server doesn't publish
// a chunk manifest.
func (p *Patcher) Run(ctx context.Context) error {
	// Prefer chunk-based patching if available
	if err := p.RunChunked(ctx); err == nil {
		return nil
	}
	// Fallback: legacy file-level patching
	return p.runLegacy(ctx)
}

// runLegacy is the original file-level verify + download pipeline.
func (p *Patcher) runLegacy(ctx context.Context) error {
	p.progress("fetching_manifest", 0, 0, "")
	manifest, err := p.fetchManifest(ctx)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	// Verify phase: compute total bytes to download
	p.progress("verifying", 0, 0, "")
	var toDownload []ManifestFile
	var totalBytes int64
	for _, f := range manifest.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		ok, _ := p.verifyFile(f)
		if !ok {
			toDownload = append(toDownload, f)
			totalBytes += f.Size
		}
	}
	if len(toDownload) == 0 {
		p.progress("up_to_date", 0, 0, "")
		return nil
	}

	// Download phase — each file retries up to legacyMaxRetry times
	// with exponential backoff + jitter on transient errors.
	var done int64
	for _, f := range toDownload {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		p.progress("downloading", done, totalBytes, f.Path)

		var lastErr error
		for attempt := 0; attempt <= legacyMaxRetry; attempt++ {
			if attempt > 0 {
				backoff := legacyRetryBase * time.Duration(1<<(attempt-1))
				jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoff + jitter):
				}
				p.progress("retrying", done, totalBytes, f.Path)
			}
			lastErr = p.downloadFile(ctx, manifest.BaseURL, f, &done, totalBytes)
			if lastErr == nil {
				break
			}
		}
		if lastErr != nil {
			return fmt.Errorf("download %s (after %d retries): %w", f.Path, legacyMaxRetry, lastErr)
		}
	}
	p.progress("complete", totalBytes, totalBytes, "")
	return nil
}

func (p *Patcher) fetchManifest(ctx context.Context) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.manifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &m, nil
}

// verifyFile returns true if the on-disk file's MD5 matches the manifest.
func (p *Patcher) verifyFile(f ManifestFile) (bool, error) {
	path := filepath.Join(p.clientRoot, f.Path)
	fh, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return false, err
	}
	// Quick check: size mismatch → definitely out of date
	if f.Size > 0 && info.Size() != f.Size {
		return false, nil
	}

	h := md5.New()
	if _, err := io.Copy(h, fh); err != nil {
		return false, err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	return actual == f.MD5, nil
}

// downloadFile atomically replaces the target with freshly downloaded bytes.
// Uses HTTP Range for resume if a partial .tmp file already exists on disk.
func (p *Patcher) downloadFile(ctx context.Context, baseURL string, f ManifestFile, done *int64, total int64) error {
	targetPath := filepath.Join(p.clientRoot, f.Path)
	tmpPath := targetPath + ".tmp"

	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	// Check for existing partial download (resume)
	var startOffset int64
	if stat, err := os.Stat(tmpPath); err == nil {
		startOffset = stat.Size()
	}

	url := baseURL + f.URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if startOffset > 0 && resp.StatusCode == 206 {
		flags |= os.O_APPEND
		*done += startOffset
	} else {
		flags |= os.O_TRUNC
		startOffset = 0 // server ignored the range
	}
	tmpFile, err := os.OpenFile(tmpPath, flags, 0o644)
	if err != nil {
		return err
	}

	// Wrap the response body in a counter so we can emit progress.
	counter := &progressWriter{
		writer:   tmpFile,
		done:     done,
		total:    total,
		progress: p.progress,
		path:     f.Path,
	}
	if _, err := io.Copy(counter, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Verify MD5 of downloaded .tmp before promoting
	if ok, err := p.verifyFile(ManifestFile{Path: f.Path + ".tmp", MD5: f.MD5, Size: f.Size}); err != nil || !ok {
		// Don't leave a bad .tmp hanging around
		os.Remove(tmpPath)
		if err == nil {
			err = errors.New("post-download MD5 mismatch")
		}
		return err
	}

	// Atomic rename: replaces any existing target
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	return nil
}

// progressWriter wraps an io.Writer and fires a progress callback every 64KB.
type progressWriter struct {
	writer   io.Writer
	done     *int64
	total    int64
	progress ProgressFn
	path     string
	counter  int
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	*w.done += int64(n)
	w.counter += n
	if w.counter > 64*1024 {
		w.progress("downloading", *w.done, w.total, w.path)
		w.counter = 0
	}
	return n, err
}
