package ethindex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello world")
	if err := store.Write(t.Context(), "testkey", data); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	loaded, err := store.Read(t.Context(), "testkey")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected data, got nil")
	}
	if string(loaded) != string(data) {
		t.Errorf("data mismatch: got %q, want %q", loaded, data)
	}
}

func TestFileStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Read(t.Context(), "missingkey")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing key, got %v", loaded)
	}
}

func TestFileStore_Move(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Write(t.Context(), "src", []byte("hello")); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	if err := store.Move(t.Context(), "src", "dst"); err != nil {
		t.Fatalf("failed to move: %v", err)
	}

	loaded, err := store.Read(t.Context(), "dst")
	if err != nil {
		t.Fatalf("failed to load moved data: %v", err)
	}
	if string(loaded) != "hello" {
		t.Errorf("expected %q, got %q", "hello", loaded)
	}

	if _, err := os.Stat(filepath.Join(dir, "src.gz")); !os.IsNotExist(err) {
		t.Errorf("expected source file to be removed, got error: %v", err)
	}
}

func TestFileStore_MoveMissingSource(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Move(t.Context(), "missing", "dst"); err == nil {
		t.Fatal("expected error moving missing source, got nil")
	}
}

func TestFileStore_WriteOverwrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Write(t.Context(), "k", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(t.Context(), "k", []byte("second")); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Read(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != "second" {
		t.Errorf("after overwrite got %q, want %q", loaded, "second")
	}
}

func TestFileStore_WriteEmptyData(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Write(t.Context(), "k", []byte{}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Read(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected empty (non-nil) slice, got nil")
	}
	if len(loaded) != 0 {
		t.Errorf("expected empty slice, got %q", loaded)
	}
}

func TestFileStore_MoveOverwritesDestination(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Write(t.Context(), "src", []byte("from-src")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(t.Context(), "dst", []byte("from-dst")); err != nil {
		t.Fatal(err)
	}

	if err := store.Move(t.Context(), "src", "dst"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	loaded, err := store.Read(t.Context(), "dst")
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != "from-src" {
		t.Errorf("after move got %q, want %q", loaded, "from-src")
	}

	if _, err := os.Stat(filepath.Join(dir, "src.gz")); !os.IsNotExist(err) {
		t.Errorf("expected source removed, got error: %v", err)
	}
}

func TestFileStore_NewFileStoreExistingDir(t *testing.T) {
	dir := t.TempDir()

	// First call creates the directory.
	if _, err := NewFileStore(dir); err != nil {
		t.Fatal(err)
	}

	// Second call on the same directory must succeed (MkdirAll is idempotent).
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("second NewFileStore: %v", err)
	}

	if err := store.Write(t.Context(), "k", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Read(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != "ok" {
		t.Errorf("got %q, want %q", loaded, "ok")
	}
}
