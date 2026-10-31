package templates

import (
	"bytes"
	"io"
	"io/fs"
	"path"

	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

type embedLoader struct {
	fsys  fs.FS
	paths map[string]bool

	inline map[string][]byte
}

func newEmbedLoader(fsys fs.FS, files []string) *embedLoader {
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f] = true
	}
	return (&embedLoader{fsys: fsys, paths: paths, inline: map[string][]byte{}}).addOwnedTemplates()
}

func (l *embedLoader) addOwnedTemplates() *embedLoader {
	for _, name := range core.OwnedTemplateNames() {
		src, _ := core.OwnedTemplate(name)
		file := name + ".jet"
		if l.paths[file] {

			logger.Scene("build").With("template", file).
				Warn("模板同时存在于集中目录与组件目录，以组件目录为准（建议删除集中目录那份）")
		}
		l.inline[file] = []byte(src)
	}
	return l
}

func (l *embedLoader) normalize(templatePath string) string {
	return path.Clean(path.Join("/", templatePath))[1:]
}

func (l *embedLoader) Exists(templatePath string) bool {
	name := l.normalize(templatePath)
	if _, ok := l.inline[name]; ok {
		return true
	}
	return l.paths[name]
}

func (l *embedLoader) Open(templatePath string) (io.ReadCloser, error) {
	name := l.normalize(templatePath)
	if src, ok := l.inline[name]; ok {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	return l.fsys.Open(name)
}
