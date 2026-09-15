package builder

// components_i18n_test.go — 组件文案与多语言声明的一致性契约（审计 I18N-019）。
//
// 多语言有两套机制，管的是不同的东西：
//   - Translatable 白名单：Props 里**作者填的**文案，经内容翻译（sys_translation）取译文；
//   - I18nAware：组件**自带的固定文案**（按钮文字 / aria-label / 空态提示），构建期取词。
//
// 两套机制缺一个都不会报错：缺 Translatable 是「作者填的文案翻不了」，
// 缺 I18nAware 是「按钮永远是中文」—— 都只在非默认语言的产物里才看得出来，
// 而不看非默认语言的站点太多了。
//
// 本测试扫全部组件模板里的**用户可见硬编码中文**（排除 Jet 注释与 HTML 注释），
// 要求该组件要么实现 I18nAware，要么在下面两张表里明确登记。
//
// 只看模板、不看 Go：Go 里的中文常常正是 I18nAware 的**兜底常量**
//（text 为 nil 时用），把它算成硬编码会得到一堆假阳性。模板里的中文文案
// 没有这层歧义 —— 它就是会烘进产物、访客会读到的那串字。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// i18nExemptions 无需 I18nAware 的组件 —— 理由必须写清，不能只写「暂不处理」。
//
// 当前为空。唯一疑似需要豁免的两例（globalref / layoutslot 模板里的
// `<!-- 全局块引用 -->`）在扫描阶段就被 HTML 注释过滤器排除了：注释不是访客可见
// 文案，进不了候选集就不需要豁免。机制保留是因为下一类豁免（纯图形组件）随时会出现。
var i18nExemptions = map[string]string{}

// i18nPending 已确认需要、尚未实现 I18nAware 的组件（归 I18N-010 逐组件补齐）。
//
// 测试断言「实际缺口 ⊆ 本表」：新增组件带文案却不实现会立刻失败，
// 而存量缺口保持**可见**（写在表里并注明归属）而不是被一条豁免悄悄盖住。
var i18nPending = map[string]string{}

var (
	cjkRe         = regexp.MustCompile(`[\p{Han}]`)
	jetCommentRe  = regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	applyI18nRe   = regexp.MustCompile(`func \([^)]*\) ApplyI18n\(`)
)

// visibleText 去掉注释后剩下的正文（注释里的中文不是访客可见文案）。
func visibleText(tpl string) string {
	tpl = jetCommentRe.ReplaceAllString(tpl, "")
	return htmlCommentRe.ReplaceAllString(tpl, "")
}

func TestComponentI18nDeclarations(t *testing.T) {
	entries, err := os.ReadDir("components")
	if err != nil {
		t.Fatalf("读取组件目录失败: %v", err)
	}
	var checked, implemented, gaps []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join("components", e.Name())
		files, derr := os.ReadDir(dir)
		if derr != nil {
			continue
		}
		var hasText, hasI18n bool
		for _, f := range files {
			data, rerr := os.ReadFile(filepath.Join(dir, f.Name()))
			if rerr != nil {
				continue
			}
			switch {
			case strings.HasSuffix(f.Name(), ".jet"):
				if cjkRe.MatchString(visibleText(string(data))) {
					hasText = true
				}
			case strings.HasSuffix(f.Name(), ".go"):
				if applyI18nRe.MatchString(string(data)) {
					hasI18n = true
				}
			}
		}
		if !hasText {
			continue // 模板里没有硬编码文案：不需要 I18nAware
		}
		checked = append(checked, e.Name())
		if hasI18n {
			implemented = append(implemented, e.Name())
			continue
		}
		if _, ok := i18nExemptions[e.Name()]; ok {
			continue
		}
		if _, ok := i18nPending[e.Name()]; ok {
			continue
		}
		gaps = append(gaps, e.Name())
	}
	if len(checked) == 0 {
		t.Fatalf("没有扫到任何含硬编码文案的组件 —— 扫描逻辑可能失效（路径或正则）")
	}
	if len(gaps) > 0 {
		sort.Strings(gaps)
		for _, name := range gaps {
			t.Errorf("组件 %s 的模板里有访客可见的硬编码中文，但既没实现 I18nAware、也不在豁免/待办表里", name)
		}
		t.Fatalf("补齐 I18nAware（推荐）或登记到 i18nExemptions / i18nPending 并写明归属")
	}
	// 登记表的反向检查：两张表里的条目必须**仍然是缺口**。
	// 组件补齐 I18nAware 之后没人删表，登记就会变成僵尸 —— 后来者看到「这里还有活」，
	// 而实际上它只是没人清理。缺口消失比缺口存在更难被发现，所以让它在测试里响。
	done := map[string]bool{}
	for _, name := range implemented {
		done[name] = true
	}
	withText := map[string]bool{}
	for _, name := range checked {
		withText[name] = true
	}
	for name := range i18nPending {
		if done[name] {
			t.Errorf("i18nPending 里的 %s 已实现 I18nAware，请从待办表删掉", name)
		}
		// 扫描根本没发现文案 = 这个登记是假的：要么文案已经被搬走、要么它本来就没有
		// 硬编码文案。两种情况下留在表里都会让人以为「这里还有活」。
		if !withText[name] {
			t.Errorf("i18nPending 里的 %s 扫描不到硬编码文案，请从待办表删掉", name)
		}
	}
	for name := range i18nExemptions {
		if done[name] {
			t.Errorf("i18nExemptions 里的 %s 已实现 I18nAware，豁免可以撤掉了", name)
		}
	}
	t.Logf("含硬编码文案的组件 %d 个（其中 %d 个已实现 I18nAware，%d 个待办，%d 个豁免）",
		len(checked), len(implemented), len(i18nPending), len(i18nExemptions))
}
