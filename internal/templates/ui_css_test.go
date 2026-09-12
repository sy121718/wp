package templates

// ui_css_test.go — ui.css 的跨投递约束。
//
// ui.css 同时服务**两个投递目标**：
//
//   后台页面 —— 有 --c-*（theme.css 定义），没有 --sky-c-*
//   构建产物 —— 有 --sky-c-*（ThemeVarsCSS 生成），没有 --c-*
//
// 所以它对任何一边的令牌引用都**必须带 fallback**：var() 无 fallback 且变量未定义时
// 整条声明被丢弃，表现为「控件在产物里丢了颜色 / 边框」这类不报错的视觉缺失。
//
// 这个约束此前被破过：45 处 var(--c-*) 写了 fallback、43 处 var(--sky-c-*) 没写 ——
// 说明作者知道后台没有 --sky-c-*，但按「产物有 --c-*」的假设漏了另一半。
// 而 --sky-c-* 与 --c-* 根本不是同一个集合（后者含 --c-text-secondary 等派生色），
// 所以补的不是别名，是各自的实际取值。

import (
	"os"
	"regexp"
	"testing"
)

// TestUICssTokenRefsHaveFallback ui.css 引用的令牌一律要带 fallback。
func TestUICssTokenRefsHaveFallback(t *testing.T) {
	css, err := os.ReadFile("static/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败: %v", err)
	}
	src := string(css)

	// var(--c-xxx) 或 var(--sky-c-xxx)：括号内没有逗号 = 没有 fallback。
	noFallback := regexp.MustCompile(`var\(\s*--(?:sky-)?c-[a-z0-9-]+\s*\)`)
	if hits := noFallback.FindAllString(src, -1); len(hits) > 0 {
		t.Errorf("ui.css 有 %d 处令牌引用缺少 fallback（另一侧投递会失效），例如 %v", len(hits), hits[:min(3, len(hits))])
		// min 为 Go 1.21+ 内置。
	}

	// 两套前缀都要真的出现，否则本约束失去意义（可能被整体改名而没更新这里）。
	if !regexp.MustCompile(`var\(\s*--c-[a-z0-9-]+\s*,`).MatchString(src) {
		t.Error("ui.css 不再引用 --c-*：若已统一令牌前缀，请同步更新本测试的说明与断言")
	}
	if !regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*,`).MatchString(src) {
		t.Error("ui.css 不再引用 --sky-c-*：若已统一令牌前缀，请同步更新本测试的说明与断言")
	}
}
