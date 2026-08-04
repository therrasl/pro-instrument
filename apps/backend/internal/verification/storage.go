package verification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type FileStorage interface {
	Save(context.Context, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type LocalFileStorage struct {
	root string
}

func NewLocalFileStorage(root string) (*LocalFileStorage, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage path: %w", err)
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	if err := os.Chmod(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("secure storage directory: %w", err)
	}
	return &LocalFileStorage{root: absoluteRoot}, nil
}

func (storage *LocalFileStorage) Save(ctx context.Context, key string, content io.Reader) error {
	path, err := storage.path(key)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create stored file: %w", err)
	}

	_, copyErr := io.Copy(file, &contextReader{ctx: ctx, reader: content})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return fmt.Errorf("write stored file: %w", copyErr)
		}
		return fmt.Errorf("close stored file: %w", closeErr)
	}

	return nil
}

func (storage *LocalFileStorage) Delete(_ context.Context, key string) error {
	path, err := storage.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete stored file: %w", err)
	}
	return nil
}

func (storage *LocalFileStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := storage.path(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open stored file: %w", err)
	}
	return file, nil
}

func (storage *LocalFileStorage) path(key string) (string, error) {
	if key == "" || filepath.Base(key) != key {
		return "", errors.New("invalid storage key")
	}
	return filepath.Join(storage.root, key), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(value []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(value)
	}
}
