package scan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var (
	// ErrNotRegular means the path is a link, a FIFO, a folder or a device.
	ErrNotRegular = errors.New("not a regular file")
	// ErrTooBig means the file is over the size limit.
	ErrTooBig = errors.New("file is too big")
)

// ReadOutFile reads dir/name, a file the model's tools wrote. Treat it as
// hostile. name must be a plain file name. The file is opened without
// following a link and without blocking on a FIFO, must be a regular file,
// and is read for at most max bytes.
func ReadOutFile(dir, name string, max int64) ([]byte, error) {
	if !plainName(name) {
		return nil, fmt.Errorf("%q is not a plain file name", name)
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: %w", name, ErrNotRegular)
		}
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, ErrNotRegular)
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s: %w", name, ErrTooBig)
	}
	// The size can grow after the Stat, so the read has its own limit.
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: %w", name, ErrTooBig)
	}
	return data, nil
}
