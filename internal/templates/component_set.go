package templates

import (
	jetengine "github.com/CloudyKit/jet/v6"

	"go_wp/pkg/view/jet"
)

// NewComponentSet 构造组件模板 Set：磁盘源 + 构建器登记的组件自带模板。
func NewComponentSet(dir string) (*jetengine.Set, error) {
	return jet.NewComponentSetWithOwned(dir, withOwnedTemplates)
}
