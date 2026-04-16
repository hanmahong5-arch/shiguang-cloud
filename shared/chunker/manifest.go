// manifest.go provides streaming JSON encoding/decoding for chunk manifests,
// with optional HMAC-SHA256 signing for integrity verification.
//
// Uses json.Decoder for streaming parse (Gemini review: don't load 200KB+
// manifest into memory as one blob). For practical manifest sizes (2500
// chunks @ 10GB client), this is a safety measure — the manifest itself
// is small, but this approach scales to 100GB+ clients gracefully.
//
// HMAC signing: the patchbuilder signs the manifest with a shared secret.
// The launcher verifies the signature before trusting the manifest's hashes.
// This prevents MitM attacks that tamper with chunk hashes to serve malicious data.
package chunker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// WriteManifest writes the manifest as pretty-printed JSON.
// If hmacKey is non-empty, computes HMAC-SHA256 over the chunk hashes
// and stores the signature in the manifest's Signature field.
func WriteManifest(path string, m *Manifest, hmacKey ...string) error {
	// If a signing key is provided, compute signature before writing
	if len(hmacKey) > 0 && hmacKey[0] != "" {
		m.Signature = computeManifestHMAC(m, hmacKey[0])
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// ReadManifest reads a manifest from a JSON file using streaming decode.
func ReadManifest(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return DecodeManifest(f)
}

// DecodeManifest reads a manifest from a reader using json.Decoder.
func DecodeManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(r)
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// VerifyManifestSignature checks the manifest's HMAC-SHA256 signature.
// Returns nil if the signature matches, or an error if it doesn't.
// If the manifest has no signature (unsigned), returns nil (backwards compatible).
func VerifyManifestSignature(m *Manifest, hmacKey string) error {
	if m.Signature == "" {
		return nil // unsigned manifest — compatible with pre-signing builds
	}
	if hmacKey == "" {
		return nil // no key configured — skip verification
	}
	expected := computeManifestHMAC(m, hmacKey)
	if !hmac.Equal([]byte(m.Signature), []byte(expected)) {
		return fmt.Errorf("manifest signature mismatch (tampered or wrong key)")
	}
	return nil
}

// computeManifestHMAC computes HMAC-SHA256 over the sorted chunk hashes.
// The signature covers: version + root + all chunk hashes (sorted).
// Sorting ensures deterministic signature regardless of chunk order.
func computeManifestHMAC(m *Manifest, key string) string {
	mac := hmac.New(sha256.New, []byte(key))

	// Include version and root in the signed payload
	mac.Write([]byte(m.Version))
	mac.Write([]byte{0}) // separator
	mac.Write([]byte(m.Root))
	mac.Write([]byte{0})

	// Sort chunk hashes for deterministic signing
	hashes := make([]string, len(m.Chunks))
	for i, c := range m.Chunks {
		hashes[i] = c.Hash
	}
	sort.Strings(hashes)
	mac.Write([]byte(strings.Join(hashes, ",")))

	return hex.EncodeToString(mac.Sum(nil))
}

// DiffManifests compares old and new manifests and returns chunks that
// are new or changed (by hash). These are the chunks the patcher needs
// to download. Returns the download list.
func DiffManifests(old, cur *Manifest) []FileChunk {
	// Build hash set of existing chunks
	existing := make(map[string]bool, len(old.Chunks))
	for _, c := range old.Chunks {
		existing[c.Hash] = true
	}

	// Find chunks in current that don't exist in old
	var diff []FileChunk
	for _, c := range cur.Chunks {
		if !existing[c.Hash] {
			diff = append(diff, c)
		}
	}
	return diff
}
