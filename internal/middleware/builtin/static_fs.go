package builtin

import (
	"net/http"
	"os"
	"path"
	"strings"
)

func NoDirListFS(root string) http.FileSystem {
	return noDirListFS{inner: http.Dir(root)}
}

type noDirListFS struct{ inner http.FileSystem }

func (fs noDirListFS) Open(name string) (http.File, error) {
	f, err := fs.inner.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {

		_ = f.Close()
		return nil, err
	}
	if !st.IsDir() {

		return f, nil
	}

	probe, err := fs.inner.Open(path.Join(strings.TrimSuffix(name, "/"), "index.html"))
	if err != nil {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	_ = probe.Close()
	return noDirListDir{f}, nil
}

type noDirListDir struct{ http.File }

func (d noDirListDir) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
