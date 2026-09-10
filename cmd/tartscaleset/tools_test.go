package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGzip(t *testing.T) {
	archive := testArchive(t, "softnet", []byte("binary"), 0o755)
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "archive.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "extracted")
	if err := extractTarGzip(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	if path := filepath.Join(destination, "softnet"); !isExecutable(path) {
		t.Fatalf("extracted file is not executable: %s", path)
	}
}

func TestExtractTarGzipRejectsTraversal(t *testing.T) {
	archive := testArchive(t, "../escape", []byte("bad"), 0o644)
	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGzip(archivePath, t.TempDir()); err == nil {
		t.Fatal("extractTarGzip accepted a path traversal entry")
	}
}

func TestEnsurePrivilegedSoftnetAttemptsSetup(t *testing.T) {
	softnet := filepath.Join(t.TempDir(), "softnet")
	if err := os.WriteFile(softnet, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensurePrivilegedSoftnet(ctx, softnet)
	if err == nil || !strings.Contains(err.Error(), "configure Softnet") {
		t.Fatalf("ensurePrivilegedSoftnet() error = %v, want setup attempt", err)
	}
}

func testArchive(t *testing.T, name string, contents []byte, mode int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
