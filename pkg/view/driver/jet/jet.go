// Package jet 是 pkg/view 的 Jet v6 驱动器实现。
//
// 两个入参，仅此而已：
//   - fsys —— 模板来源（embed.FS 或磁盘目录的子目录）。多来源（如组件自带模板）
//     由调用方合成一个 fs.FS 传入，引擎不关心；
//   - funcs —— 全局**纯函数**（money / date / fill …），进程级注册一次。
//
// **不要**往 funcs 放依赖请求上下文的函数（如按请求语言取词的 t）：Jet 的 Set 是
// 进程级单例，那样会跨请求串号。请求级数据由调用方放进每次渲染的 vars。
//
// 扩展名在驱动内部定死（"" / .html / .jet 都认），后台页与片段的模板因此共用同一个引擎。
package jet

import (
	"bytes"
	"errors"
	"io"
	"io/fs"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/view/driver"
)

// defaultExts 驱动认的模板名扩展名（Jet 会依次尝试）。
var defaultExts = []string{"", ".html", ".jet"}

// fsLoader 把 fs.FS 适配为 jet.Loader。
type fsLoader struct{ fsys fs.FS }

func (l fsLoader) Exists(p string) bool {
	_, err := fs.Stat(l.fsys, p)
	return err == nil
}

func (l fsLoader) Open(p string) (io.ReadCloser, error) { return l.fsys.Open(p) }

type jetDriver struct {
	set  *jet.Set
	fsys fs.FS
}

// New 构造 Jet 驱动器。
func New(fsys fs.FS, funcs map[string]any) (driver.Driver, error) {
	if fsys == nil {
		return nil, errors.New("view/jet: 模板文件系统为空")
	}
	set := jet.NewSet(fsLoader{fsys: fsys}, jet.WithTemplateNameExtensions(defaultExts))
	for name, fn := range funcs {
		set.AddGlobal(name, fn)
	}
	return &jetDriver{set: set, fsys: fsys}, nil
}

func (d *jetDriver) Render(name string, vars map[string]any) ([]byte, error) {
	t, err := d.set.GetTemplate(name)
	if err != nil {
		return nil, err
	}
	// 先在缓冲里渲染完再返回：模板可能在输出一部分后失败，半截内容不该出去。
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil, vars); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (d *jetDriver) Exists(name string) bool {
	for _, ext := range defaultExts {
		if _, err := fs.Stat(d.fsys, name+ext); err == nil {
			return true
		}
	}
	return false
}
