package builder

import (
	"go_wp/pkg/builder/core"

	"github.com/google/uuid"
)

func ClonePageWithNewIDs(page *Page) *Page {
	if page == nil {
		return nil
	}
	cloned := *page
	cloned.Root = core.CloneNodes(page.Root, func(string) string { return uuid.NewString() })
	return &cloned
}

func CloneNodeWithNewIDs(n *core.Node) *core.Node {
	return core.CloneNodes([]*core.Node{n}, func(string) string { return uuid.NewString() })[0]
}
