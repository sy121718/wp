package jet

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"strings"
)

// Loader 是 Jet 模板查找的最小接口。
type Loader interface {
	Exists(templatePath string) bool
	Open(templatePath string) (io.ReadCloser, error)
}

// FSLoader 按逻辑键在给定的 fs.FS 上查找模板。
//
// inline 用于框架/应用在装配时提供的进程内模板源（例如组件自带模板），
// 与磁盘/embed 源合并；同名冲突以 inline 为准并可由装配方显式报错。
type FSLoader struct {
	fsys   fs.FS
	paths  map[string]bool
	inline map[string][]byte
}

// NewFSLoader 基于文件清单构造 loader；files 是相对 fsys 的逻辑键。
func NewFSLoader(fsys fs.FS, files []string) *FSLoader {
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f] = true
	}
	return &FSLoader{fsys: fsys, paths: paths, inline: map[string][]byte{}}
}

// WithInline 追加进程内模板源；name 为逻辑键。
func (l *FSLoader) WithInline(name string, src []byte) *FSLoader {
	if l.inline == nil {
		l.inline = map[string][]byte{}
	}
	l.inline[name] = src
	return l
}

// HasInlineConflict 报告逻辑键是否同时存在于文件源与 inline 源。
func (l *FSLoader) HasInlineConflict(name string) bool {
	_, inInline := l.inline[name]
	return inInline && l.paths[name]
}

func (l *FSLoader) normalize(templatePath string) string {
	return strings.TrimPrefix(path.Clean(path.Join("/", templatePath)), "/")
}

func (l *FSLoader) Exists(templatePath string) bool {
	name := l.normalize(templatePath)
	if _, ok := l.inline[name]; ok {
		return true
	}
	return l.paths[name]
}

func (l *FSLoader) Open(templatePath string) (io.ReadCloser, error) {
	name := l.normalize(templatePath)
	if src, ok := l.inline[name]; ok {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	return l.fsys.Open(name)
}
