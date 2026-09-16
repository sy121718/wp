package templates

// backend_css_naming_contract_test.go — 后台静态样式的令牌命名收口（审计 UIK-003 第四层）。
//
// 前三层（令牌命名统一 / 片段样式归基座 / 基座按控件切分注入）落地后，后台**自有的静态样式**
// 仍在直接消费 --c-* 前缀（theme.css 100 行 / workbench.css 195 行 / media-lib.css 36 行 /
// workbench-a11y.css 6 行）：同一个后台色板槽，基座写 --sky-c-*、后台页面样式写 --c-*，
// 两边都能改一处而另一边静默不跟随 —— 命名空间没有真正收口，这正是第四层要收的那一段。
// 现在消费端统一到 --sky-c-*，取值仍由 theme.css 的 [data-theme] 别名段从 --c-* 转供，
// 所以是「改命名、不改取值」：后台亮/暗主题下的解析结果与改前逐字节相同。
//
// 三条约束（都在这里机器判据化）：
//  ① 后台静态样式不得出现 var(--c-*) —— 唯一例外是 theme.css 的 [data-theme] 别名段，
//     它本来就是 --c-* → --sky-c-* 的转供点；
//  ② 引用到的 --sky-c-* 槽必须有来源，来源只有三种：别名段供给 / 本文件自有定义 /
//     显式登记的无供给槽（backendTokensWithoutSupplier，逐条写清为什么它没有供给者）；
//  ③ 自有定义的 --sky-c-* 槽必须真的被引用（防死定义）。
//
// 与 ui_css_test.go 的分工：那里钉「基座（ui.css）只写一个命名空间 + 每处带兜底 +
// 基座引用与别名段双向对表」；这里钉「后台静态样式侧同样只写一个命名空间，且引用不悬空」。
// 对表的**覆盖范围**由本文件提供的 backendStaticAliasConsumers 扩展到后台静态样式。

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// backendStaticCSSFiles 后台静态样式（不进产物，由 admin 页面 <link> 引入）。
// ui.css 不在此列：它同时投递产物，约束见 ui_css_test.go。
var backendStaticCSSFiles = []string{
	"static/css/theme.css",
	"static/css/workbench.css",
	"static/css/media-lib.css",
	"static/css/workbench-a11y.css",
}

// reSkyTokenDef 抓 --sky-c-x 的定义行（本文件内的自有定义）。
var reSkyTokenDef = regexp.MustCompile(`(?m)^\s*[ \t]*(--sky-c-[a-z0-9-]+)\s*:`)

func readBackendStaticCSS(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", rel, err)
	}
	return string(raw)
}

// backendStaticSkyRefs 每个文件引用到的 --sky-c-* 槽（已剥注释，且对 theme.css 去掉别名段 ——
// 别名段里的 var(--c-x) 是转供，不是消费）。
func backendStaticSkyRefs(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, rel := range backendStaticCSSFiles {
		src := stripThemeAliasBlock(uiCssStripComments(readBackendStaticCSS(t, rel)))
		refs := map[string]bool{}
		for _, m := range reAnyTokenRef.FindAllStringSubmatch(src, -1) {
			if strings.HasPrefix(m[1], "--sky-c-") {
				refs[m[1]] = true
			}
		}
		out[rel] = refs
	}
	return out
}

// backendStaticOwnedTokens 每个文件**自有定义**的 --sky-c-* 槽（定义在同一文件里，
// 例如 workbench.css 的 --sky-c-canvas-bg —— 它是工作台的局部量，不属后台共享色板，
// 因此不进别名段；它改成 --sky-* 命名空间的理由是「后台侧只留一个命名空间」）。
func backendStaticOwnedTokens(t *testing.T) map[string]bool {
	t.Helper()
	owned := map[string]bool{}
	for _, rel := range backendStaticCSSFiles {
		src := stripThemeAliasBlock(uiCssStripComments(readBackendStaticCSS(t, rel)))
		for _, m := range reSkyTokenDef.FindAllStringSubmatch(src, -1) {
			owned[m[1]] = true
		}
	}
	return owned
}

// stripThemeAliasBlock 去掉 theme.css 的 [data-theme] 别名段（其余文件原样返回）。
func stripThemeAliasBlock(src string) string {
	if block := reDataThemeBlock.FindString(src); block != "" {
		return strings.Replace(src, block, "", 1)
	}
	return src
}

// backendTokensWithoutSupplier 后台静态样式引用、但**两端都没有供给者**的槽。
//
// 它们的共同点是「引用了不存在的名字」：改名前写成 var(--c-warn, 兜底) 时同样永远取兜底
// （--c-warn 从来没有定义过），改名后行为逐字节不变 —— 本条收口只统一命名，不顺手改值。
// 登记在这里而不是就地修掉，是因为修它等于改值（视觉会变），要与主题口径一起决策：
//
//	--sky-c-warn       → 后台色板只有 --c-warning（亮 #8a6d3b / 暗 #c2a06a），没有 --c-warn；
//	                     两处兜底分别是 #b45309（workbench.css:688）与 #f59e0b（theme.css:426）。
//	                     指向 --sky-c-warning 会让颜色真的变，属产品决策。
//	--sky-c-text-muted → 几乎肯定是 --sky-c-text-mute 的拼写偏差（同一文件里两种写法并存）；
//	                     亮色下兜底与 mute 同值（#6b7280），暗色下会由写死的 #6b7280 变为
//	                     #8a9199 —— 这其实正是「修好」，但仍是视觉变更，按待决处理。
//	--sky-c-text-dim   → 后台色板最淡的一档是 --c-text-faint（#9ca3af / 暗色 #5d656e），
//	                     没有 dim 档；workbench.css:1123 的兜底 #8a93a3 在写死状态下工作。
var backendTokensWithoutSupplier = map[string]string{
	"--sky-c-warn":       "后台色板无 --c-warn（只有 --c-warning）：两处引用改名前同样永远取兜底",
	"--sky-c-text-muted": "后台色板无 --c-text-muted（应为 --c-text-mute）：改名前同样永远取兜底",
	"--sky-c-text-dim":   "后台色板无 dim 档（最淡是 --c-text-faint）：改名前同样永远取兜底",
}

// backendStaticAliasConsumers 后台静态样式里**需要别名段供给**的槽集合。
// ui_css_test.go 的对表断言用它把覆盖范围从 ui.css 扩展到后台静态样式。
func backendStaticAliasConsumers(t *testing.T) map[string]bool {
	t.Helper()
	owned := backendStaticOwnedTokens(t)
	out := map[string]bool{}
	for _, refs := range backendStaticSkyRefs(t) {
		for name := range refs {
			if owned[name] {
				continue
			}
			if _, registered := backendTokensWithoutSupplier[name]; registered {
				continue
			}
			out[name] = true
		}
	}
	return out
}

// TestBackendStaticCSSUsesSingleNamespace 后台静态样式只写 --sky-* 命名空间（UIK-003 第四层）。
func TestBackendStaticCSSUsesSingleNamespace(t *testing.T) {
	total := 0
	for _, rel := range backendStaticCSSFiles {
		src := stripThemeAliasBlock(uiCssStripComments(readBackendStaticCSS(t, rel)))
		if hits := reBackendTokenRef.FindAllString(src, -1); len(hits) > 0 {
			t.Errorf("%s 仍有 %d 处后台令牌直接引用 %v：后台静态样式与基座共用同一个命名空间，"+
				"取值由 theme.css 的 [data-theme] 别名段统一转供（UIK-003 第四层）",
				rel, len(hits), hits[:min(3, len(hits))])
		}
		for _, m := range reAnyTokenRef.FindAllStringSubmatch(src, -1) {
			if strings.HasPrefix(m[1], "--sky-c-") {
				total++
			}
		}
	}
	// 解析口径自检：一条都没解析到说明正则或文件结构变了，本约束会在空转中通过。
	if total < 80 {
		t.Errorf("后台静态样式只解析出 %d 处 --sky-c-* 引用（预期 ≥80）：解析口径可能已失效", total)
	}
}

// TestBackendStaticCSSTokensHaveSupplier 后台静态样式的每个引用都有来源（三分区闭合）。
func TestBackendStaticCSSTokensHaveSupplier(t *testing.T) {
	aliases := backendAliases(t)
	owned := backendStaticOwnedTokens(t)
	refs := backendStaticSkyRefs(t)

	referenced := map[string]bool{}
	var unsourced []string
	for rel, set := range refs {
		for name := range set {
			referenced[name] = true
			if _, ok := aliases[name]; ok {
				continue
			}
			if owned[name] {
				continue
			}
			if _, ok := backendTokensWithoutSupplier[name]; ok {
				continue
			}
			unsourced = append(unsourced, rel+" → "+name)
		}
	}
	sort.Strings(unsourced)
	if len(unsourced) > 0 {
		t.Errorf("这些引用没有来源 %v：槽必须有供给者（别名段 / 本文件自有定义 / backendTokensWithoutSupplier 登记），"+
			"否则一旦别名段漏条就会静默退化成写死的亮色值", unsourced)
	}

	// 反向一：登记的无供给槽必须真的被引用（僵尸登记会让清单失去可信度）。
	for name := range backendTokensWithoutSupplier {
		if !referenced[name] {
			t.Errorf("backendTokensWithoutSupplier 登记的 %s 已无人引用：请从登记表里删掉", name)
		}
		if _, ok := aliases[name]; ok {
			t.Errorf("%s 已在别名段供给，登记表里的「无供给」说明已过期", name)
		}
		if owned[name] {
			t.Errorf("%s 已由后台静态样式自定义，登记表里的「无供给」说明已过期", name)
		}
	}

	// 反向二：解析口径自检（避免「引用集合为空 → 空转通过」）。
	if len(referenced) < 10 {
		t.Fatalf("后台静态样式只引用到 %d 个槽（预期 ≥10）：解析口径可能已失效", len(referenced))
	}
}

// TestBackendStaticCSSOwnedTokensAreReferenced 自有定义必须被引用（防死定义）。
func TestBackendStaticCSSOwnedTokensAreReferenced(t *testing.T) {
	owned := backendStaticOwnedTokens(t)
	refs := backendStaticSkyRefs(t)
	referenced := map[string]bool{}
	for _, set := range refs {
		for name := range set {
			referenced[name] = true
		}
	}
	var dead []string
	for name := range owned {
		if !referenced[name] {
			dead = append(dead, name)
		}
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Errorf("这些槽在后台静态样式里定义却无人引用 %v：死定义应当删除", dead)
	}
}

// ── 页面级专用类的归位判据（UIK-003 第四层的另一半） ─────────────────────────
//
// pages-* 桥接类在前一批已迁到基座公共类（pages_class_migration_test.go 钉住模板侧、
// ui_css_ownership_test.go 钉住 theme.css 侧）。本批复核确认迁移完整，并补上「基座侧零回流」
// 这个缺口 —— 既有断言只查 .pages-form / .attr-form-head 三个前缀，漏掉其它 .pages-* 选择器。
//
// 归位判据（前三层落地后固定下来，新增样式先按它判归属）：
//   · 可复用的控件外观 → ui.css 基座（btn / card / form-* / table-wrap / badge / dot /
//     pagination / drawer，以及工作台的 wb-* 都已在基座）；
//   · 单页面私有的布局与排版（网格、窄屏堆叠、max-width 收口、按页命名的容器）→ 留在页面级
//     （theme.css 里带页面前缀的段，或页面内联 <style>）；
//   · 构建期组件的几何与布局 → 组件作用域（.sky-c-{nodeId}），不进基座。
// 据此，theme.css 与模板内联样式里的 source- / purchase- / masterdata- / pricing- / attr- /
// var- / tr- / locale- / theme- / move- / receipt- / tag-rule- 段均属「页面私有布局」，
// 归位结论是留在页面级；它们的类名带页面前缀，正是为了让归属一眼可判。
// 两处已知待决（本批只登记、不机械迁移，理由见报告）：
//   · .pagination-btn 在 theme.css（顶层版）与 ui.css（.pagination-pages 后代版）各写一版：
//     后代版特异性更高因而大部分生效，但顶层版仍有 3 项声明在起作用（hover 的 color、
//     is-disabled 的 color、transition 里的 color）—— 合并要动基座（改产物字节 + golden），
//     属基座层内部收口，需单独一批。
//   · .sky-ip-*（图标选择器整套）只服务后台 WPIcons.picker，按「可复用控件外观」判据应评估
//     是否进基座；当前留在 theme.css，等口径确认。

// TestUICSSCarriesNoLegacyPagesSelectors 基座里不得再有 .pages-* 选择器（零回流）。
func TestUICSSCarriesNoLegacyPagesSelectors(t *testing.T) {
	sels := uiCssSelectors(readBackendStaticCSS(t, "static/css/ui.css"))
	if len(sels) < 50 {
		t.Fatalf("ui.css 只解析出 %d 个选择器（预期 ≥50）：解析口径可能已失效", len(sels))
	}
	for _, sel := range sels {
		if strings.Contains(sel, ".pages-") {
			t.Errorf("ui.css 仍有旧桥接选择器 %q：pages-* 已按归位判据迁到基座公共类，"+
				"基座里不该再出现这个前缀（防回流，UIK-003 第四层）", sel)
		}
	}
}
