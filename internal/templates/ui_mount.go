package templates

import (
	"fmt"
	"io/fs"
	"os"
	"sort"

	jetpkg "go_wp/pkg/view/jet"
	"go_wp/pkg/view/ui"
)

// 公共 UI 模板属主在 pkg/view/ui，业务模板留在应用。
// 两者按逻辑键挂进同一复合 FS：先查框架，再查应用；同名即冲突，显式报错，
// 不做逐模板 fallback 搜索，也不允许应用覆盖公共控件形成第二套 UI。

// compositeFS 是按优先级叠加的只读模板源。
type compositeFS struct {
	layers []layer
}

type layer struct {
	name string
	fsys fs.FS
}

func newUICompositeFS(app fs.FS) (*compositeFS, error) {
	c := &compositeFS{layers: []layer{
		{name: "pkg/view/ui", fsys: ui.TemplatesFS()},
		{name: "app", fsys: app},
	}}
	if err := c.checkConflicts(); err != nil {
		return nil, err
	}
	return c, nil
}

// checkConflicts 在装配期发现重名逻辑键，而不是运行时静默选一份。
func (c *compositeFS) checkConflicts() error {
	seen := map[string]string{}
	for _, l := range c.layers {
		err := fs.WalkDir(l.fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if prev, dup := seen[p]; dup {
				return fmt.Errorf("模板逻辑键 %q 同时存在于 %s 与 %s（每个逻辑键只能有一个属主）", p, prev, l.name)
			}
			seen[p] = l.name
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *compositeFS) Open(name string) (fs.File, error) {
	for _, l := range c.layers {
		f, err := l.fsys.Open(name)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) && !isNotExist(err) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func isNotExist(err error) bool {
	return err == fs.ErrNotExist
}

// listAll 返回复合 FS 的全部文件逻辑键。
func (c *compositeFS) listAll() ([]string, error) {
	set := map[string]bool{}
	for _, l := range c.layers {
		err := fs.WalkDir(l.fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !d.IsDir() {
				set[p] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	files := make([]string, 0, len(set))
	for p := range set {
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}

// devAppFS 把开发期的磁盘模板目录包装成 fs.FS。
func devAppFS(dir string) fs.FS { return os.DirFS(dir) }

// newDevTemplateLoader 用复合 FS 构造开发期 loader（公共 UI + 磁盘业务模板）。
func newDevTemplateLoader(viewDir string) (*jetpkg.FSLoader, error) {
	composite, err := newUICompositeFS(devAppFS(viewDir))
	if err != nil {
		return nil, err
	}
	files, err := composite.listAll()
	if err != nil {
		return nil, err
	}
	return withOwnedTemplates(jetpkg.NewFSLoader(composite, files)), nil
}

// testTemplateLoader 是包内测试用的开发期 loader：与生产同一复合 FS。
func testTemplateLoader() jetpkg.Loader {
	loader, err := newDevTemplateLoader(".")
	if err != nil {
		panic("测试模板 loader 构建失败: " + err.Error())
	}
	return loader
}

// readAll 读取复合 FS 中一个逻辑键的全部内容。
func readAll(fsys fs.FS, name string) ([]byte, error) {
	return fs.ReadFile(fsys, name)
}
