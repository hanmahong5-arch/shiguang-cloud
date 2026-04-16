// patchbuilder generates a chunk manifest and chunk files from a game client directory.
//
// Usage:
//   patchbuilder -client D:\aion-client -out ./patch
//
// This scans the client directory, splits every file into 4MB chunks,
// computes SHA-256 for each chunk, and writes both the manifest JSON
// and content-addressable chunk files to the output directory.
//
// Output layout:
//   patch/
//     chunk-manifest.json
//     chunks/
//       {sha256_hash_1}
//       {sha256_hash_2}
//       ...
//
// Example workflow:
//   1. Update game client files locally
//   2. Run: patchbuilder -client ./client -out ./patch
//   3. Copy patch/ directory to Hub's patch_dir or upload to CDN
//   4. Set the manifest URL in the Hub dashboard (Branding → Patch Manifest URL)
//   5. Launchers auto-download changed chunks on next startup
//
// For incremental updates: re-run with the same -out directory.
// Only new/changed chunks are written (content-addressable dedup).
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/shiguang/shared/chunker"
)

func main() {
	var clientDir string
	var outDir string
	var manifestOnly bool
	var signKey string
	flag.StringVar(&clientDir, "client", "", "path to the game client directory (required)")
	flag.StringVar(&outDir, "out", "patch", "output directory for manifest + chunk files")
	flag.BoolVar(&manifestOnly, "manifest-only", false, "only generate manifest, skip chunk export")
	flag.StringVar(&signKey, "sign-key", "", "HMAC-SHA256 key to sign the manifest (anti-tamper)")
	flag.Parse()

	if clientDir == "" {
		log.Fatal("usage: patchbuilder -client <client-dir> [-out ./patch] [-manifest-only] [-sign-key SECRET]")
	}

	start := time.Now()

	if manifestOnly {
		// Legacy mode: only generate manifest JSON (no chunk files)
		log.Printf("scanning %s (manifest-only mode)...", clientDir)
		manifest, err := chunker.BuildManifest(clientDir)
		if err != nil {
			log.Fatalf("build manifest: %v", err)
		}
		manifestPath := outDir + "/chunk-manifest.json"
		if err := chunker.WriteManifest(manifestPath, manifest, signKey); err != nil {
			log.Fatalf("write manifest: %v", err)
		}
		if signKey != "" {
			fmt.Println("  Signed:  HMAC-SHA256 ✓")
		}
		printManifestSummary(manifest, manifestPath, time.Since(start))
		return
	}

	// Full export: manifest + chunk files
	log.Printf("scanning %s → exporting to %s...", clientDir, outDir)
	stats, err := chunker.ExportChunks(clientDir, outDir, signKey)
	if err != nil {
		log.Fatalf("export: %v", err)
	}

	elapsed := time.Since(start)

	fmt.Printf("\nExport complete → %s/\n", outDir)
	fmt.Printf("  Chunks total: %d\n", stats.TotalChunks)
	fmt.Printf("  New written:  %d\n", stats.NewChunks)
	fmt.Printf("  Dedup skip:   %d\n", stats.SkippedDup)
	fmt.Printf("  Raw size:     %.2f GB\n", float64(stats.TotalBytes)/(1024*1024*1024))
	fmt.Printf("  Time:         %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("\nNext: copy %s/ to Hub patch_dir or CDN\n", outDir)
}

// printManifestSummary outputs manifest-only stats (legacy mode).
func printManifestSummary(m *chunker.Manifest, path string, elapsed time.Duration) {
	files := map[string]bool{}
	var size int64
	for _, c := range m.Chunks {
		files[c.Path] = true
		size += int64(c.Size)
	}
	fmt.Printf("\nManifest written to %s\n", path)
	fmt.Printf("  Files:   %d\n", len(files))
	fmt.Printf("  Chunks:  %d\n", len(m.Chunks))
	fmt.Printf("  Size:    %.2f GB\n", float64(size)/(1024*1024*1024))
	fmt.Printf("  Time:    %v\n", elapsed.Round(time.Millisecond))
}
