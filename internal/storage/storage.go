package storage

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideRoot = errors.New("storage: path escapes the root")

type Disk struct {
	root string
	url  string
}

func New(root, url string) (*Disk, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, err
	}
	return &Disk{root: absolute, url: strings.TrimSuffix(url, "/")}, nil
}

func (d *Disk) resolve(name string) (string, error) {
	clean := filepath.Join(d.root, filepath.Clean("/"+name))
	if !strings.HasPrefix(clean, d.root+string(os.PathSeparator)) && clean != d.root {
		return "", ErrOutsideRoot
	}

	root, err := filepath.EvalSymlinks(d.root)
	if err != nil {
		return "", err
	}

	probe := clean
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if real != root && !strings.HasPrefix(real, root+string(os.PathSeparator)) {
				return "", ErrOutsideRoot
			}
			return clean, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(probe)
		if parent == probe {
			return "", ErrOutsideRoot
		}
		probe = parent
	}
}

func (d *Disk) Put(name string, reader io.Reader) (int64, error) {
	path, err := d.resolve(name)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}

	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return io.Copy(file, reader)
}

func (d *Disk) PutBytes(name string, content []byte) error {
	path, err := d.resolve(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func (d *Disk) Get(name string) ([]byte, error) {
	path, err := d.resolve(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (d *Disk) Exists(name string) bool {
	path, err := d.resolve(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (d *Disk) Delete(name string) error {
	path, err := d.resolve(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (d *Disk) Size(name string) (int64, error) {
	path, err := d.resolve(name)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (d *Disk) URL(name string) string {
	return d.url + "/" + strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+name)), "/")
}

func (d *Disk) Root() string { return d.root }
