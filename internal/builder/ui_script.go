package builder

// ui_script.go — 原始控件基座（js/ui/）的按需拼装。
//
// 与 enhance.js 的关系：enhance.js 是**组件级**增强（轮播/灯箱/卡片环，按 data-* 挑块），
// 这里是**原始控件级**（下拉/输入/标签页…，按 data-ui-* 挑块）。两者都会内联进产物，
// 但来源与关注点不同，所以各有一份拼装逻辑，共用同一套「特征命中才注入」的思路。
//
// 源文件在 internal/templates/static/js/ui/：运行时经 /static 给后台页面，
// 构建期由装配层读出后注入（builder 不依赖 internal/templates）。

import (
	"strings"

	"go_wp/pkg/logger"
)

// uiBlock 一个可独立注入的原始控件（文件名对应 static/js/ui/<file>）。
type uiBlock struct {
	// file 控件实现文件名（不含路径）。
	file string
	// feats 触发注入的产物特征（任一命中即注入）。
	feats []string
}

// uiBlocks 控件清单，顺序即输出顺序。新增控件时必须同时登记特征 ——
// 漏登记会让用到它的页面静默失去该控件的增强（与 enhanceBlocks 同一条约定）。
var uiBlocks = []uiBlock{
	{file: "select.js", feats: []string{"data-ui-select"}},
	// 弹窗：特征取 "data-modal"，同时命中 data-modal-open / data-modal-close ——
	// 页面只要出现任一弹窗触发点，就该带上这个控件。
	{file: "modal.js", feats: []string{"data-modal"}},
}

// uiStyleFor 取该产物需要的控件样式（有控件命中才返回，纯内容页为空）。
//
// css 由装配层注入（static/css/ui.css）。控件脚本进了产物却没样式，
// 访客看到的就是没有外观的空壳 —— 所以两者必须同进同出。
func uiStyleFor(html, css string) string {
	if strings.TrimSpace(css) == "" {
		return ""
	}
	probe := scriptTagRe.ReplaceAllString(html, "")
	for _, b := range uiBlocks {
		for _, f := range b.feats {
			if strings.Contains(probe, f) {
				return css
			}
		}
	}
	return ""
}

// uiScriptFor 按产物 HTML 拼装需要的原始控件：基座助手 + 命中的控件 + 入口。
//
// sources 为文件名 → 源码（装配层从 embed 读出）。一个控件都没命中时返回空 ——
// 纯内容页不该为空增强付流量。基座（_util.js）与入口（index.js）只在有控件时注入。
func uiScriptFor(html string, sources map[string]string) string {
	if len(sources) == 0 {
		return ""
	}
	probe := scriptTagRe.ReplaceAllString(html, "")

	var parts []string
	for _, b := range uiBlocks {
		hit := false
		for _, f := range b.feats {
			if strings.Contains(probe, f) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		src := sources[b.file]
		if strings.TrimSpace(src) == "" {
			// 登记了控件却没有源码：产物会少一个控件的交互，必须留痕而不是静默。
			logger.Scene("build").With("file", b.file).Warn("原始控件源码缺失，产物将不含该控件增强")
			continue
		}
		parts = append(parts, src)
	}
	if len(parts) == 0 {
		return ""
	}
	// 顺序固定：助手 → 各控件 → 入口（入口负责扫描与 htmx 重扫，必须最后）。
	//
	// 只返回脚本正文，**不带 <script> 标签** —— document.jet 已在外层套了 <script>，
	// 这里再套一次会拼成 `</script><script>` 嵌套，整段脚本语法错误、全部失效
	//（enhanceScriptFor 同样只返回正文，两者拼接后才进模板）。
	var sb strings.Builder
	sb.WriteString(sources["_util.js"])
	for _, p := range parts {
		sb.WriteString("\n")
		sb.WriteString(p)
	}
	sb.WriteString("\n")
	sb.WriteString(sources["index.js"])
	return sb.String()
}
