package templates

// ui_css_delivery_boundary_test.go — 基座令牌的**投递边界**（审计 UIK-003 第一层 / UIK-004）。
//
// ui.css 同时投递给两个目标，而两边能拿到的令牌**不是同一个集合**：
//
//	后台 / 工作台 —— theme.css 的 [data-theme] 提供后台色板 --c-*（22 个，含派生色），
//	                 基座引用到的那部分由同一段里的别名 --sky-c-* 转供；
//	构建产物      —— builder.ThemeVarsCSS 按主题生成 --sky-c-*（集合随主题设置变化，
//	                 没配主色就根本没有 --sky-c-primary）。
//
// 命名统一（UIK-003）解决的是「基座只写一套名字」，不解决「两端集合不同」——
// 后者由兜底吸收：每个 var(--sky-c-X, 兜底) 在缺 X 时用兜底值。
//
// 本文件钉住三件事：
//
//	① 别名段必须在 [data-theme] 里，不能在 :root —— --c-* 由 [data-theme] 定义，
//	   同一元素上解析才不会出现「别名先解析、主题值后到达」的空窗；
//	② 产物侧与后台侧**同名**的那批槽（productTokens 下）后台必须都有别名：
//	   它们就是「两端共用一份基座」的直接体现（产物有值、后台也有值，取值各自提供）；
//	③ 只服务单端的派生槽（如 --sky-c-bg-hover）在两侧的差异被明确列出，
//	   不靠「以后再说」搪塞 —— 清单变动会让这条断言失败，逼作者更新说明。
//
// productTokens 的权威来源是 internal/builder/theme_settings.go 的 themeVars()，
// builder 包有一个反向断言（背靠背）保证这份副本不会与生成器漂移。

import (
	"regexp"
	"strings"
	"testing"
)

// productTokens 产物侧 ThemeVarsCSS 会生成的色板令牌名（去掉 --sky- 前缀后的色板部分）。
var productTokens = []string{
	"c-primary", "c-secondary", "c-accent", "c-success", "c-warning", "c-danger",
	"c-text", "c-heading", "c-bg", "c-surface", "c-border",
}

// TestBaseTokenAliasLivesInDataTheme 别名段的选择器必须是 [data-theme]。
func TestBaseTokenAliasLivesInDataTheme(t *testing.T) {
	src := themeCssSource(t)
	block := reDataThemeBlock.FindString(src)
	if block == "" {
		t.Fatal("theme.css 缺少 [data-theme] 块")
	}
	if !strings.Contains(block, "--sky-c-primary:") {
		t.Fatalf("基座令牌别名不在 [data-theme] 块里：--c-* 与别名必须同一元素上解析，否则后台会先取到兜底值")
	}
	// 反向：别名不该出现在 :root 块里（那里只有不依赖主题的不变量）。
	root := regexp.MustCompile(`(?s):root\s*\{[^}]*\}`).FindString(src)
	if strings.Contains(root, "--sky-c-primary:") {
		t.Error("基座令牌别名出现在 :root 块里：[data-theme] 未命中的页面上会静默落到兜底值")
	}
}

// TestSharedTokensAcrossDeliveries 「两端共用同一批名字」的实证。
//
// 只有**基座真的引用到**的产物令牌才算共享：产物生成器能产出 --sky-c-heading /
// --sky-c-accent 这类名字，但基座不消费它们，就不该为了整齐给后台补一条没人用的别名
// （死别名会让「哪些槽还受支持」变含糊 —— ui_css_test.go 的反向断言盯着这一点）。
func TestSharedTokensAcrossDeliveries(t *testing.T) {
	used := skyTokensInUICSS(t)
	aliases := backendAliases(t)
	var shared, missing []string
	for _, name := range productTokens {
		full := "--sky-" + name
		if !used[full] {
			continue
		}
		shared = append(shared, full)
		if _, ok := aliases[full]; !ok {
			missing = append(missing, full)
		}
	}
	if len(shared) < 5 {
		t.Errorf("基座引用的产物侧令牌只有 %d 个（预期 ≥5）：两端可能退化成各写一套命名空间", len(shared))
	}
	if len(missing) > 0 {
		t.Errorf("这些令牌基座在引用、产物侧也生成，后台却没有别名 %v：\n"+
			"「两端共用一份基座」要求同名同时可用（取值可以不同），缺别名的那几个在后台只会落兜底值", missing)
	}
}

// skyTokensInUICSS 基座里引用到的 --sky-c-* 槽集合（已剥注释）。
func skyTokensInUICSS(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range reAnyTokenRef.FindAllStringSubmatch(uiCssStripComments(uiCssSource(t)), -1) {
		if strings.HasPrefix(m[1], "--sky-c-") {
			out[m[1]] = true
		}
	}
	return out
}

// derivedOnlyTokens 只在后台侧存在（产物侧没有等价物）的槽。
//
// 它们的存在不是缺陷：产物侧这些语义由主题生成器的其它字段表达（或干脆没有），
// 基座对它们的引用一律带兜底 —— 产物侧走兜底，后台侧走后台色板。
// 清单写在这里是为了让「两端集合不同」这件事随时可查，而不是留给下次审计重新发现。
var derivedOnlyTokens = map[string]string{
	"--sky-c-primary-deep":   "产物侧的深色主色由组件级配色决定，不生成全站令牌",
	"--sky-c-primary-bg":     "产物侧的浅色主色底由组件级配色决定（按钮/徽标各自的底色），不生成全站令牌",
	"--sky-c-primary-soft":   "产物侧没有中间档主色（主题只有 primary / secondary / accent）",
	"--sky-c-bg-hover":       "产物侧不用 hover 底色表达状态（悬停形态由组件自己声明）",
	"--sky-c-bg-soft":        "产物侧对应的是 --sky-c-surface（主题的 Surface 字段）",
	"--sky-c-text-secondary": "产物侧次要文本色没有独立令牌（正文/标题两档）",
	"--sky-c-text-mute":      "产物侧对应的是 --sky-c-accent（点缀色）",
	"--sky-c-text-faint":     "产物侧没有更淡的一档文本色",
	"--sky-c-border-strong":  "产物侧只有单一边框色",
	"--sky-c-border-input":   "产物侧表单控件复用 --sky-c-border",
	"--sky-c-success-bg":     "产物侧不生成语义底色",
	"--sky-c-warning-bg":     "产物侧不生成语义底色",
	"--sky-c-danger-bg":      "产物侧不生成语义底色",
}

// TestDerivedOnlyTokensAreDocumented 单端槽必须被显式记录（清单与别名段一一对应）。
func TestDerivedOnlyTokensAreDocumented(t *testing.T) {
	aliases := backendAliases(t)
	product := map[string]bool{}
	for _, n := range productTokens {
		product["--sky-"+n] = true
	}
	for name := range aliases {
		if product[name] {
			continue
		}
		if _, ok := derivedOnlyTokens[name]; !ok {
			t.Errorf("别名 %s 既不在产物侧令牌清单里、也没有在 derivedOnlyTokens 里说明：\n"+
				"新增令牌时请说明它是「两端同名」还是「只服务某一端」，否则下一轮审计要重新推一遍", name)
		}
	}
	for name := range derivedOnlyTokens {
		if _, ok := aliases[name]; !ok {
			t.Errorf("derivedOnlyTokens 里的 %s 已不在别名段中：清单与实现分叉，请同步", name)
		}
	}
}
