package ethindex

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BlobStore stores named blobs.
type BlobStore interface {
	// Read returns the blob identified by key.
	Read(key string) ([]byte, error)

	// Write stores data under key.
	Write(key string, data []byte) error

	// Move moves a blob from oldKey to newKey.
	Move(oldKey, newKey string) error

	// Delete removes the blob identified by key.
	Delete(key string) error
}

// FileBlobStore stores blobs as gzip-compressed files.
type FileBlobStore struct {
	dir string
}

var _ BlobStore = (*FileBlobStore)(nil)

func NewFileBlobStore(dir string) (*FileBlobStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating directory %q: %w", dir, err)
	}
	return &FileBlobStore{dir: dir}, nil
}

func (fb *FileBlobStore) path(name string) string {
	return filepath.Join(fb.dir, name)
}

// Delete deletes a blob.
func (fb *FileBlobStore) Delete(key string) error {
	return os.Remove(fb.path(key))
}

// Move renames a blob.
func (fb *FileBlobStore) Move(oldKey string, newKey string) error {
	return os.Rename(fb.path(oldKey), fb.path(newKey))
}

// Read returns the decompressed contents of a blob.
func (fb *FileBlobStore) Read(key string) ([]byte, error) {
	f, err := os.Open(fb.path(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gr.Close() }()

	return io.ReadAll(gr)
}

// Write atomically stores a blob.
func (fb *FileBlobStore) Write(key string, data []byte) error {
	f, err := os.CreateTemp(fb.dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	gw := gzip.NewWriter(f)
	if _, err := gw.Write(data); err != nil {
		_ = gw.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}
	return os.Rename(f.Name(), fb.path(key))
}
