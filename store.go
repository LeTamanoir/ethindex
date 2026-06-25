package ethindex

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileStore implements Store using files in a directory.
type FileStore struct {
	dir string
}

var _ Store = (*FileStore)(nil)

// NewFileStore creates a FileStore rooted at dir.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating directory %q: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) path(key string) string {
	return filepath.Join(s.dir, key+".gz")
}

func (s *FileStore) Read(_ context.Context, key string) ([]byte, error) {
	f, err := os.Open(s.path(key))
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

func (s *FileStore) Write(_ context.Context, key string, data []byte) error {
	return atomicWrite(s.path(key), func(w io.Writer) error {
		gw := gzip.NewWriter(w)
		if _, err := gw.Write(data); err != nil {
			_ = gw.Close()
			return err
		}
		return gw.Close()
	})
}

func (s *FileStore) Move(_ context.Context, srcKey, dstKey string) error {
	if err := os.Rename(s.path(srcKey), s.path(dstKey)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("move %q: %w", srcKey, err)
		}
		return fmt.Errorf("move %q to %q: %w", srcKey, dstKey, err)
	}
	return nil
}

// atomicWrite writes to a temp file, fsyncs it, then renames it over
// filename so the destination never appears partially written.
func atomicWrite(filename string, write func(io.Writer) error) error {
	dir := filepath.Dir(filename)

	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}
