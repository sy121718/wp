package pipeline

// compile_assets.go — 手工页面、自动发布与默认编译共享的客户端资源装配。
//
// enhance.js 放在 internal/templates/static/js/ 下：运行时经 /static 给后台页面用，
// 构建期由这里取出交给 builder 按需内联，业务模块不再维护各自的文件清单。
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

	trackSourceOnce sync.Once
	trackSourceVal  string

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

// trackSource 取流量来源采集脚本源码（internal/templates/static/js/track.js）。
//
// 与 enhanceSource 分开放：两者的注入策略不同（增强按特征挑块、采集每页无条件带上），
// 合成一个变量会让 builder 那边只能二选一。
//
// 读不到时返回空串并告警：产物不含采集脚本，订单归因为空 —— 页面与下单都不受影响。
func trackSource() string {
	trackSourceOnce.Do(func() {
		js, err := templates.StaticJS("track.js")
		if err != nil {
			logger.Scene("build").Error(err, "读取流量采集脚本失败（产物将不含归因采集）")
			return
		}
		trackSourceVal = js
	})
	return trackSourceVal
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

// ClientAssetOptions 提供所有发布入口共同使用的客户端资源。
// 源码只从 embed 读取一次；是否进入产物由 builder 按实际 HTML 决定。
func ClientAssetOptions() []builder.CompileOption {
	return []builder.CompileOption{
		builder.WithEnhanceSource(enhanceSource()),
		builder.WithTrackSource(trackSource()),
		builder.WithUISources(uiSources()),
		builder.WithUIStyle(templates.UICSS()),
	}
}
