package patching

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func md5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

func TestPatcher_RunNoChangesNeeded(t *testing.T) {
	content := []byte("hello patch world 12345")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Version: "1",
		Files: []ManifestFile{
			{Path: "test.bin", MD5: md5Hex(content), Size: int64(len(content)), URL: "file"},
		},
	}
	var srvRef *httptest.Server
	srvRef = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			manifest.BaseURL = srvRef.URL + "/"
			json.NewEncoder(w).Encode(manifest)
			return
		}
		if r.URL.Path == "/file" {
			w.Write(content)
			return
		}
		w.WriteHeader(404)
	}))
	defer srvRef.Close()

	var calls []string
	p := NewPatcher(dir, srvRef.URL+"/manifest.json", func(phase string, done, total int64, file string) {
		calls = append(calls, phase)
	})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	var sawUpToDate bool
	for _, p := range calls {
		if p == "up_to_date" {
			sawUpToDate = true
			break
		}
	}
	if !sawUpToDate {
		t.Errorf("phases=%v want up_to_date", calls)
	}
}

func TestPatcher_DownloadsFreshFile(t *testing.T) {
	content := []byte("fresh download bytes!")
	dir := t.TempDir()

	manifest := Manifest{
		Version: "1",
		Files: []ManifestFile{
			{Path: "fresh.bin", MD5: md5Hex(content), Size: int64(len(content)), URL: "file"},
		},
	}
	var srvRef *httptest.Server
	srvRef = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			manifest.BaseURL = srvRef.URL + "/"
			json.NewEncoder(w).Encode(manifest)
			return
		}
		if r.URL.Path == "/file" {
			w.Write(content)
			return
		}
		w.WriteHeader(404)
	}))
	defer srvRef.Close()

	p := NewPatcher(dir, srvRef.URL+"/manifest.json", nil)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "fresh.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

func TestPatcher_MD5MismatchRejected(t *testing.T) {
	content := []byte("real content")
	dir := t.TempDir()

	manifest := Manifest{
		Version: "1",
		Files: []ManifestFile{
			{Path: "bad.bin", MD5: "0000000000000000000000000000aaaa", Size: int64(len(content)), URL: "file"},
		},
	}
	var srvRef *httptest.Server
	srvRef = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			manifest.BaseURL = srvRef.URL + "/"
			json.NewEncoder(w).Encode(manifest)
			return
		}
		if r.URL.Path == "/file" {
			w.Write(content)
			return
		}
		w.WriteHeader(404)
	}))
	defer srvRef.Close()

	p := NewPatcher(dir, srvRef.URL+"/manifest.json", nil)
	err := p.Run(context.Background())
	if err == nil {
		t.Error("expected MD5 mismatch error")
	}
}
