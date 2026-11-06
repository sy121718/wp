// Package editor 提供可视化编辑器的通用运行机制：编辑器资产、编辑区域模板、
// 检查器字段编排、大纲树渲染与内容哈希。
//
// 本包不 import internal，也不知道具体应用的保存 / 发布端点、权限码、工程表或
// 后台 URL。业务侧通过窄端口注入：检查器实体引用选项、导航新建能力、保存 /
// 发布 / 预览 / 冲突确认宿主操作，以及完整组件注册后的生成契约（palette /
// alignedRepeaters）。应用宿主资产与模板经同一复合 FS 挂载，逻辑键唯一。
package editor

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:assets
var assetsFS embed.FS

// AssetsFS 返回编辑器静态资产源，逻辑键与 /static URL 一致：
// js/editor/...、css/editor.css、css/editor-a11y.css。
//
// 应用把本 FS 与公共 UI、业务专用资产合进唯一 /static 复合源；同名逻辑键在
// 装配期报错，不做逐层回退。
func AssetsFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("pkg/builder/editor: 编辑器资产 embed 子目录失败: " + err.Error())
	}
	return sub
}

// AssetFile 按逻辑键读取一个编辑器资产。
func AssetFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(AssetsFS(), name)
	if err != nil {
		return nil, fmt.Errorf("读取编辑器资产 %s: %w", name, err)
	}
	return b, nil
}
