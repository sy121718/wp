package pipeline

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

func ClientAssetOptions() []builder.CompileOption {
	return []builder.CompileOption{
		builder.WithEnhanceSource(enhanceSource()),
		builder.WithTrackSource(trackSource()),
		builder.WithUISources(uiSources()),
		builder.WithUIStyle(templates.UICSS()),
	}
}
