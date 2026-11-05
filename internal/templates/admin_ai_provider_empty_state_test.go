package templates

import (
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
)

// 这一组守的是「AI 供应商空态明明 hidden 却还显示」这条真实缺陷。它有两层：
//
//	CSS 层：.list-card { display: flex } 是作者样式，压过浏览器 UA 的 [hidden]{display:none}，
//	        于是给 .list-card 元素加 hidden 属性完全无效 —— DOM 上 hidden 是真、开发者工具
//	        里看不出问题，屏幕上照样占位。
//	模板层：空态得在有卡时真的带 hidden、零卡时真的不带。CSS 写对了但模板永远不 hidden，
//	        用户在有卡时依然看到「还没有配置任何 AI 供应商」；反过来零卡时也永远看不到这句。
//
// 两侧都要测：只测属性不算「真实可见性回归」，只测 CSS 又管不住模板。

// TestListCardHiddenRuleBeatsFlexDisplay 断言 theme.css 里有能真正压住 display: flex 的
// [hidden] 守卫，且位置在 .list-card 基础规则之后（.list-card[hidden] 与 .list-card 特异性
// 不同，但同类守卫一律写在基础规则后面才不会被后续声明覆盖）。
func TestListCardHiddenRuleBeatsFlexDisplay(t *testing.T) {
	css := readAdminThemeCSS(t)
	base, _ := splitCSSMedia(css)

	if body := ruleBody(base, ".list-card {"); !strings.Contains(body, "display: flex") {
		t.Fatalf(".list-card 不再声明 display: flex（本用例前提已变，需重新判断空态问题是否还存在）")
	}

	iBase := strings.Index(base, ".list-card {")
	iGuard := strings.Index(base, ".list-card[hidden]")
	if iGuard < 0 {
		t.Fatal("theme.css 缺少 .list-card[hidden] 规则：UA 的 [hidden]{display:none} 会被 .list-card 的 " +
			"display:flex 压掉，带 hidden 的列表卡（AI 供应商空态）照样显示")
	}
	if iGuard < iBase {
		t.Error(".list-card[hidden] 写在 .list-card 之前：后续基础规则会把它覆盖掉，等于没写")
	}

	guard := base[iGuard:]
	if end := strings.Index(guard, "}"); end < 0 || !strings.Contains(guard[:end], "display: none") {
		t.Fatalf(".list-card[hidden] 未声明 display: none，实际：%s", strings.SplitN(guard, "}", 2)[0])
	}
}

// TestAIProviderEmptyStateHiddenTracksCardCount 走真实 SSR：有卡时空态必须带 hidden，
// 零卡时不能带。两种状态都要走同一份模板与同一份夹具，差别只在 ProviderCards。
func TestAIProviderEmptyStateHiddenTracksCardCount(t *testing.T) {
	const marker = `data-ai-provider-empty`

	emptyAttr := func(t *testing.T, body string) string {
		t.Helper()
		i := strings.Index(body, marker)
		if i < 0 {
			t.Fatal("会话页没有渲染供应商空态元素（data-ai-provider-empty）")
		}
		// 取到该标签的下一个 '>' 为止，判断里面有没有 hidden 属性。
		rest := body[i:]
		end := strings.Index(rest, ">")
		if end < 0 {
			t.Fatalf("空态元素标签未闭合：%s", rest[:80])
		}
		return rest[:end]
	}

	// 空态整块在 {{if canViewModels}} 里：不给这个权限，模板根本渲染不到那段，
	// 断言会退化成「找不到标记」而不是「hidden 不对」。两个标签开关都要开。
	modelsPage := func(cards []any) map[string]any {
		data := sessionPageBase()
		data["CanViewModels"] = true
		data["IsModels"] = true
		data["ProviderCards"] = cards
		// 新建供应商抽屉里 range 了 ProtocolOptions；缺了它模板直接报
		// "no field or method 'ProtocolOptions'"，与空态断言无关地失败。
		data["ProtocolOptions"] = aienums.ProtocolOptions
		data["Buttons"] = map[string]bool{"ai.provider_save": true}
		return data
	}

	t.Run("有卡片时空态不显示", func(t *testing.T) {
		tag := emptyAttr(t, renderSessionPage(t, modelsPage([]any{providerCardFixture()})))
		if !strings.Contains(tag, "hidden") {
			t.Fatalf("有卡片时空态必须带 hidden，否则「还没有配置任何 AI 供应商」会显示在卡片旁边；实际标签：%s", tag)
		}
	})

	t.Run("零卡片时空态显示", func(t *testing.T) {
		tag := emptyAttr(t, renderSessionPage(t, modelsPage([]any{})))
		if strings.Contains(tag, "hidden") {
			t.Fatalf("零卡片时空态不该带 hidden，否则用户看到一片空白；实际标签：%s", tag)
		}
	})
}

// providerCardFixture 造一张最小的供应商卡：只要模板不因缺字段报错，空态侧的断言就成立。
// 字段名按 ProviderCardResp 与 cardData 的真实形状给，不猜模板里的取值。
func providerCardFixture() map[string]any {
	return map[string]any{
		"Provider": aidto.Provider{
			ID: 1, ProviderKey: "groq", DisplayName: "Browser Groq",
			BaseURL: "https://api.groq.com/openai/v1", Protocol: "openai-chat-completions",
			Status: 1, Models: []aidto.ModelEntry{}, HasAPIKey: true, Version: 2,
		},
		"Rows":            []aidto.ModelRowResp{},
		"ProtocolOptions": aienums.ProtocolOptions,
		"DefaultBaseURL":  "",
		"BuiltinKeys":     []string{},
		"StatusTone":      "ok",
		"CSRFToken":       "tok",
		"T":               TranslateFunc("zh-CN"),
		"PermSet":         map[string]bool{},
		"Buttons": map[string]bool{
			"ai.provider_save": true, "ai.provider_status": true, "ai.provider_delete": true,
			"ai.provider_models_save": true, "ai.provider_models_restore": true,
			"ai.provider_models_candidates": true, "ai.provider_models_append": true,
		},
	}
}
