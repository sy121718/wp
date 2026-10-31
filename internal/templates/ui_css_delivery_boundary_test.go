package templates

import (
	"regexp"
	"strings"
	"testing"
)

var productTokens = []string{
	"c-primary", "c-secondary", "c-accent", "c-success", "c-warning", "c-danger",
	"c-text", "c-heading", "c-bg", "c-surface", "c-border",
}

func TestBaseTokenAliasLivesInDataTheme(t *testing.T) {
	src := themeCssSource(t)
	block := reDataThemeBlock.FindString(src)
	if block == "" {
		t.Fatal("theme.css 缺少 [data-theme] 块")
	}
	if !strings.Contains(block, "--sky-c-primary:") {
		t.Fatalf("基座令牌别名不在 [data-theme] 块里：--c-* 与别名必须同一元素上解析，否则后台会先取到兜底值")
	}

	root := regexp.MustCompile(`(?s):root\s*\{[^}]*\}`).FindString(src)
	if strings.Contains(root, "--sky-c-primary:") {
		t.Error("基座令牌别名出现在 :root 块里：[data-theme] 未命中的页面上会静默落到兜底值")
	}
}

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

var derivedOnlyTokens = map[string]string{
	"--sky-c-primary-deep":   "产物侧的深色主色由组件级配色决定，不生成全站令牌",
	"--sky-c-primary-bg":     "产物侧的浅色主色底由组件级配色决定（按钮/徽标各自的底色），不生成全站令牌",
	"--sky-c-primary-soft":   "产物侧没有中间档主色（主题只有 primary / secondary / accent）",
	"--sky-c-bg-hover":       "产物侧不用 hover 底色表达状态（悬停形态由组件自己声明）",
	"--sky-c-bg-active":      "产物侧不用按压底色表达状态；后台侧原本就有 --c-bg-active，只是此前没有别名，UIK-003 第四层收口时补上（消费方是 theme.css 的 .nav-toggle:active）",
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
