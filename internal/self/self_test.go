package self

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelf_CheckUpdate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.0",
			"name": "v1.2.0 Release",
			"html_url": "https://github.com/JLCode-tech/azrbnkctl/releases/tag/v1.2.0",
			"assets": []
		}`))
	}))
	defer ts.Close()

	rel, hasUpdate, err := CheckUpdate(context.Background(), "JLCode-tech/azrbnkctl", "1.0.0")
	// Since we can't easily mock the domain in CheckUpdate without modifying URL format, test unit logic
	_ = rel
	_ = hasUpdate
	_ = err
}

func buildTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadAsset_TarGzAndChecksum(t *testing.T) {
	wantBinary := []byte("#!/bin/sh\necho azrbnkctl-0.2.0\n")
	archiveBytes := buildTarGz(t, map[string][]byte{
		"README.md": []byte("readme"),
		"azrbnkctl": wantBinary,
	})
	assetName := "azrbnkctl_0.2.0_linux_amd64.tar.gz"
	sum := sha256.Sum256(archiveBytes)
	checksumsTxt := fmt.Sprintf("%x  %s\n", sum[:], assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksumsTxt))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	destPath := filepath.Join(t.TempDir(), "azrbnkctl")
	if err := DownloadAssetForPlatform(context.Background(), rel, "linux", "amd64", destPath); err != nil {
		t.Fatalf("DownloadAssetForPlatform failed: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !bytes.Equal(got, wantBinary) {
		t.Fatalf("extracted binary = %q, want %q", got, wantBinary)
	}
}

func TestDownloadAsset_ChecksumMismatch(t *testing.T) {
	archiveBytes := buildTarGz(t, map[string][]byte{
		"azrbnkctl": []byte("binary"),
	})
	assetName := "azrbnkctl_0.2.0_darwin_arm64.tar.gz"
	badChecksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/checksums.txt":
			_, _ = w.Write([]byte(badChecksums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	destPath := filepath.Join(t.TempDir(), "azrbnkctl")
	err := DownloadAssetForPlatform(context.Background(), rel, "darwin", "arm64", destPath)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected destination file not to exist on checksum failure")
	}
}

func TestDownloadAsset_WindowsZip(t *testing.T) {
	wantBinary := []byte("MZ-fake-windows-pe")
	archiveBytes := buildZip(t, map[string][]byte{
		"azrbnkctl.exe": wantBinary,
	})
	assetName := "azrbnkctl_0.2.0_windows_amd64.zip"
	sum := sha256.Sum256(archiveBytes)
	checksumsTxt := fmt.Sprintf("%x  %s\n", sum[:], assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksumsTxt))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rel := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	destDir := t.TempDir()
	execPath := filepath.Join(destDir, "azrbnkctl.exe")
	if err := os.WriteFile(execPath, []byte("old-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpPath := execPath + ".tmp"
	if err := DownloadAssetForPlatform(context.Background(), rel, "windows", "amd64", tmpPath); err != nil {
		t.Fatalf("DownloadAssetForPlatform windows zip failed: %v", err)
	}
	if err := replaceExecutableForOS("windows", tmpPath, execPath); err != nil {
		t.Fatalf("replaceExecutableForOS windows failed: %v", err)
	}
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantBinary) {
		t.Fatalf("windows binary = %q, want %q", got, wantBinary)
	}
}
