package templates

import (
	"go_wp/pkg/builder/core"
	"go_wp/pkg/logger"
	"go_wp/pkg/view/jet"
)

// withOwnedTemplates 把构建器登记的组件自带模板合并进 loader。
//
// 逻辑键为 <name>.jet；同名冲突以组件模板为准并告警。
// 引擎本身不认识构建器，这一步只在应用侧装配时发生。
func withOwnedTemplates(loader *jet.FSLoader) *jet.FSLoader {
	for _, name := range core.OwnedTemplateNames() {
		src, _ := core.OwnedTemplate(name)
		file := name + ".jet"
		if loader.HasInlineConflict(file) {
			logger.Scene("build").With("template", file).
				Warn("模板同时存在于集中目录与组件目录，以组件目录为准（建议删除集中目录那份）")
		}
		loader.WithInline(file, []byte(src))
	}
	return loader
}
