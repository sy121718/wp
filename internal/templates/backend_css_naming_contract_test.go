package templates

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var backendStaticCSSFiles = []string{
	"static/css/theme.css",
	"static/css/editor.css",
	"static/css/media-lib.css",
	"static/css/editor-a11y.css",
}

var reSkyTokenDef = regexp.MustCompile(`(?m)^\s*[ \t]*(--sky-c-[a-z0-9-]+)\s*:`)

func readBackendStaticCSS(t *testing.T, rel string) string {
	t.Helper()
	b, err := AssetFile(strings.TrimPrefix(rel, "static/"))
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", rel, err)
	}
	return string(b)
}

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

func stripThemeAliasBlock(src string) string {
	if block := reDataThemeBlock.FindString(src); block != "" {
		return strings.Replace(src, block, "", 1)
	}
	return src
}

var backendTokensWithoutSupplier = map[string]string{
	"--sky-c-warn":       "后台色板无 --c-warn（只有 --c-warning）：两处引用改名前同样永远取兜底",
	"--sky-c-text-muted": "后台色板无 --c-text-muted（应为 --c-text-mute）：改名前同样永远取兜底",
	"--sky-c-text-dim":   "后台色板无 dim 档（最淡是 --c-text-faint）：改名前同样永远取兜底",
}

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

	if total < 80 {
		t.Errorf("后台静态样式只解析出 %d 处 --sky-c-* 引用（预期 ≥80）：解析口径可能已失效", total)
	}
}

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

	if len(referenced) < 10 {
		t.Fatalf("后台静态样式只引用到 %d 个槽（预期 ≥10）：解析口径可能已失效", len(referenced))
	}
}

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
