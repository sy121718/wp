package pageservice

// page_enhance.go — 客户端增强脚本的读取（一份源文件，两个出口）。
//
// enhance.js 放在 internal/templates/static/js/ 下：运行时经 /static 给后台页面用，
// 构建期由这里取出交给 builder 内联进静态产物（builder 不依赖 templates，所以走注入）。
// embed 读取是内存操作，进程内读一次即可。

import (
	"sync"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

var (
	enhanceSourceOnce sync.Once
	enhanceSourceVal  string

	uiSourcesOnce sync.Once
	uiSourcesVal  map[string]string
)

// enhanceSource 取客户端增强脚本源码（internal/templates/static/js/enhance.js）。
//
// 读不到时返回空串：builder 会输出空增强并告警（页面照常渲染，只是失去交互），
// 而不是让整页构建失败 —— 增强是渐进能力，不该阻断内容发布。
func enhanceSource() string {
	enhanceSourceOnce.Do(func() {
		js, err := templates.StaticJS("enhance.js")
		if err != nil {
			logger.Scene("build").Error(err, "读取客户端增强脚本失败（产物将不含交互脚本）")
			return
		}
		enhanceSourceVal = js
	})
	return enhanceSourceVal
}

// uiSources 取原始控件基座源码（文件名 → 源码）。
//
// 始终返回非 nil map，表示生产装配启用控件增强。读不到的文件不进 map，
// builder 仅在页面实际使用该控件时返回资源缺失错误，不影响无关内容页。
func uiSources() map[string]string {
	uiSourcesOnce.Do(func() {
		uiFiles := builder.UIAssetFiles()
		uiSourcesVal = make(map[string]string, len(uiFiles))
		for _, name := range uiFiles {
			js, err := templates.StaticJS("ui/" + name)
			if err != nil {
				logger.Scene("build").Error(err, "读取原始控件源码失败（使用该控件的页面将构建失败）")
				continue
			}
			uiSourcesVal[name] = js
		}
	})
	return uiSourcesVal
}
