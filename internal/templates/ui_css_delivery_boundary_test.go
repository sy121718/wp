package templates

// ui_css_delivery_boundary_test.go — ui.css 两套令牌的投递边界契约（审计条目 UIK-004）。
//
// ui.css 同时投递给两个目标，而两边能拿到的令牌**不是同一个集合**：
//
//	后台 / 工作台 —— theme.css 的 [data-theme] 块提供 --c-*（22 个，含派生色）
//	构建产物      —— builder.ThemeVarsCSS 按主题生成 --sky-c-*（集合随主题设置变化，
//	                 没配主色就根本没有 --sky-c-primary）
//
// 所以对 --sky-c-* 的引用要写三段桥：
//
//	var(--sky-c-X, var(--c-X, 兜底值))
//	    ^^^^^^^^^^^  ^^^^^^^^^  ^^^^^^^^
//	    产物命中      后台命中    两边都缺时
//
// ui_css_test.go 钉住的是"必须有兜底"（括号里得有逗号）。本文件钉更细的一层：
// 兜底位**必须是 --c-* 引用**，不要直接落字面量。少了中间层，后台深色主题下这条
// 声明会退化成亮色写死值 —— 主色落到 #3d444f、背景是 #101318，焦点轮廓等于消失。
// 这处偏差真实存在过：.wbs-trigger / .wb-modal-x / .wbc-swatch 的 focus ring 用两层
// 写法，而同一文件的 .btn:focus-visible 用三层，两种写法对同一个令牌给出不同来源。

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// reSkyTokenRef 抓 var(--sky-c-xxx, <兜底段>)：捕获第一段逗号之后的内容。
// 嵌套引用形如 var(--sky-c-border, var(--c-border-input, #c6ccd4)) → 捕获 "var(--c-border-input"。
var reSkyTokenRef = regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*,\s*([^,)]+)`)

// reSkyTokenName 从整段匹配里取令牌名本身（豁免表的键）。
var reSkyTokenName = regexp.MustCompile(`--sky-c-[a-z0-9-]+`)

// skyTokenLiteralFallbackAllowed 允许兜底位直接写字面量的 --sky-c-* 令牌。
//
// 当前为空：每个 --sky-c-* 在后台都有语义对应物。将来若真出现产物独有的令牌，
// 在这里登记令牌名并注明理由，而不是放宽断言 —— 放宽会让这层保护整体失效。
var skyTokenLiteralFallbackAllowed = map[string]bool{}

// TestUICssSkyTokensBridgeToBackend ui.css 引用的 --sky-c-* 一律要经 --c-* 中间层。
func TestUICssSkyTokensBridgeToBackend(t *testing.T) {
	raw, err := os.ReadFile("static/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败: %v", err)
	}
	src := string(raw)

	// 守卫：ui.css 的令牌引用按单行书写（本文件格式如此）。整文件命中数必须与
	// 逐行命中数一致，否则说明出现了跨行引用，逐行检查会漏判。
	total := len(reSkyTokenRef.FindAllStringSubmatch(src, -1))
	if total < 40 {
		t.Fatalf("ui.css 只解析出 %d 处 --sky-c-* 引用（预期 ≥40）：解析口径可能已失效", total)
	}

	var offenders []string
	lineHits := 0
	for i, line := range strings.Split(src, "\n") {
		for _, m := range reSkyTokenRef.FindAllStringSubmatch(line, -1) {
			lineHits++
			seg := strings.TrimSpace(m[1])
			if strings.HasPrefix(seg, "var(--c-") {
				continue
			}
			if token := reSkyTokenName.FindString(m[0]); skyTokenLiteralFallbackAllowed[token] {
				continue
			}
			offenders = append(offenders, fmt.Sprintf("ui.css:%d → %s（兜底位是 %q，不是 var(--c-*)）",
				i+1, strings.TrimSpace(line), seg))
		}
	}
	if lineHits != total {
		t.Fatalf("有 %d 处 --sky-c-* 引用跨行书写（整文件 %d / 逐行 %d）：逐行检查会漏判，请改成单行",
			total-lineHits, total, lineHits)
	}
	if len(offenders) > 0 {
		t.Errorf("ui.css 有 %d 处 --sky-c-* 缺 --c-* 中间层（后台深色主题下会退化成写死的亮色值）:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestUICssTokenBridgeKeepsBothSidesReal ui.css 仍同时服务两个投递目标。
//
// 这条是"防呆"：若哪天有人真把两套前缀统一了，上面的断言会因为"只剩一侧"而空转通过。
// 它同时也是 docs/02-G-component-library.md §P6 结论的机器可读副本。
func TestUICssTokenBridgeKeepsBothSidesReal(t *testing.T) {
	raw, err := os.ReadFile("static/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败: %v", err)
	}
	src := string(raw)

	skyRefs := regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+`).FindAllString(src, -1)
	backendRefs := regexp.MustCompile(`var\(\s*--c-[a-z0-9-]+`).FindAllString(src, -1)
	if len(skyRefs) == 0 {
		t.Error("ui.css 不再引用 --sky-c-*：产物侧投递已断，或令牌前缀被统一（请同步 docs/02-G-component-library.md §P6 与本文件说明）")
	}
	if len(backendRefs) == 0 {
		t.Error("ui.css 不再引用 --c-*：后台侧投递已断，或令牌前缀被统一（请同步 docs/02-G-component-library.md §P6 与本文件说明）")
	}
}
