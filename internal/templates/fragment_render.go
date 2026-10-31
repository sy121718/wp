package templates

import (
	"bytes"
	"embed"
	"io/fs"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

//go:embed fragments/*.jet
var fragmentsFS embed.FS

var (
	fragmentSetOnce sync.Once
	fragmentSet     *jet.Set
	fragmentSetErr  error
)

func fragmentTemplateSet() (*jet.Set, error) {
	fragmentSetOnce.Do(func() {
		loader, err := newFragmentLoader()
		if err != nil {
			fragmentSetErr = err
			return
		}
		set := jet.NewSet(
			loader,
			jet.WithTemplateNameExtensions([]string{"", ".jet"}),
		)
		injectGlobals(set)
		fragmentSet = set
	})
	return fragmentSet, fragmentSetErr
}

func newFragmentLoader() (*embedLoader, error) {
	sub, err := fs.Sub(fragmentsFS, "fragments")
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return newEmbedLoader(sub, files), nil
}

func RenderFragment(name string, data any) (string, error) {
	set, err := fragmentTemplateSet()
	if err != nil {
		return "", err
	}
	t, err := set.GetTemplate(name)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, nil, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
