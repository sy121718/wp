package templates

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
)

//go:embed all:admin
var adminTemplatesFS embed.FS

//go:embed all:static
var staticAssetsFS embed.FS

func newAdminTemplateLoader() (*embedLoader, error) {
	files, err := listEmbeddedFiles(adminTemplatesFS)
	if err != nil {
		return nil, err
	}
	return newEmbedLoader(adminTemplatesFS, files), nil
}

func EmbeddedStaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(staticAssetsFS, "static")
	if err != nil {
		return nil, err
	}
	return noDirListFS{inner: http.FS(sub)}, nil
}

func listEmbeddedFiles(fsys fs.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

type noDirListFS struct{ inner http.FileSystem }

func (f noDirListFS) Open(name string) (http.File, error) {
	file, err := f.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return noDirListFile{File: file}, nil
}

type noDirListFile struct{ http.File }

func (f noDirListFile) Readdir(int) ([]fs.FileInfo, error) {
	return nil, errors.New("目录列表已禁用")
}
