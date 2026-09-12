package core

// keyframes_query.go — 按名取关键帧（供**后台页面按需内联**）。
//
// 产物侧的按需由 CSSBuckets.NeedKeyframes 负责（构建期逐条激活）。
// 后台没有 props 管线，但同样不该常驻加载 63 条营销动效 ——
// 所以给一条「页面声明要哪几个，才内联哪几个」的通道：
//
//     data["KeyframesCSS"] = core.KeyframeCSS([]string{"sky-fade-up"})
//
// 声明为空 → 返回空串 → layout 不输出任何 <style>（零字节）。

import (
	"strings"

	"go_wp/pkg/logger"
)

// KeyframeCSS 取指定动效词汇的关键帧规则体，按传入顺序拼接（重复输入只取一次）。
//
// 未知名字跳过并留痕：后台页面是开发者写的，写错名字属于笔误，
// 静默失败会让人以为「动画没效果是浏览器的锅」。
func KeyframeCSS(names []string) string {
	if len(names) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(names))
	parts := make([]string, 0, len(names))
	for _, n := range names {
		name := strings.TrimSpace(n)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		css, ok := keyframeIndex[name]
		if !ok {
			logger.Scene("admin").With("keyframe", name).
				Warn("后台页面请求了不存在的动效词汇，已跳过（检查拼写，见 core/keyframes/*.css）")
			continue
		}
		parts = append(parts, css)
	}
	return strings.Join(parts, "\n")
}

// KeyframeNames 返回全部可用的动效词汇名（字典序），供后台做选择器或文档。
func KeyframeNames() []string {
	names := make([]string, 0, len(keyframesCatalog))
	for _, k := range keyframesCatalog {
		names = append(names, k.Name)
	}
	return names
}
