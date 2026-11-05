package templates

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"

	"go_wp/pkg/view/jet"
)

//go:embed all:admin all:fragments all:workbench all:partials
var appTemplatesFS embed.FS

func newAdminTemplateLoader() (*jet.FSLoader, error) {
	// 公共 UI 在 pkg/view/ui，业务模板在应用的 embed；按逻辑键合成单一 FS。
	composite, err := newUICompositeFS(appTemplatesFS)
	if err != nil {
		return nil, err
	}
	files, err := composite.listAll()
	if err != nil {
		return nil, err
	}
	return withOwnedTemplates(jet.NewFSLoader(composite, files)), nil
}

func EmbeddedStaticFS() (http.FileSystem, error) {
	assets, err := StaticAssets()
	if err != nil {
		return nil, err
	}
	return noDirListFS{inner: http.FS(assets)}, nil
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
