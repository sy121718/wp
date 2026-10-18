package productservice

// product_message_test.go — 写动作回执的文案合成守卫（跳过原因兜底 + 当前语言一致性）。
//
// 合成逻辑在 product_message.go（service）：控制器只把结果交给 text.MsgText。
// 因此守卫也在这里 —— 留在 inbound/http 就只能测到自己抄的那份副本。

import (
	"fmt"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

// TestVariantSkipFallbacksCoverEnumsSkipKeys 每条跳过原因都要有展示原文。
//
// 跳过原因不走 facing 的业务判据（取值就是常量名），enums 那层不给它们配兜底 ——
// variantSkipFallbacks 是唯一真源，漏一条页面上就是裸 key（VariantSkipHasStock 这种串）。
func TestVariantSkipFallbacksCoverEnumsSkipKeys(t *testing.T) {
	all := enumsStringConstantsOf(t)
	covered := 0
	for val, name := range all {
		if !strings.HasPrefix(name, "VariantSkip") {
			continue
		}
		covered++
		text := strings.TrimSpace(variantSkipFallbacks[val])
		if text == "" {
			t.Errorf("%s (=%q) 没有中文兜底，页面上会显示裸 key", name, val)
			continue
		}
		if text == val {
			t.Errorf("%s (=%q) 的兜底就是 key 本身 —— 等于没有兜底", name, val)
		}
	}
	if covered == 0 {
		t.Fatal("没扫到任何 VariantSkip* 常量 —— 守卫本身失效了")
	}
	// 反向：表里的 key 必须仍是 enums 里声明的常量（改名后没同步 → 这张表退化成死条目）。
	for key := range variantSkipFallbacks {
		if _, ok := all[key]; !ok {
			t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
		}
	}
}

// messageInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var messageInjectedEntries = map[string]map[string]string{
	productenums.BulkVariantNoChange: {"zh-CN": "变体清单与库里一致，没有需要保存的变化。", "en-US": "EN-VARIANT-NOCHANGE"},
	productenums.BulkVariantSaved:    {"zh-CN": "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。", "en-US": "EN-VARIANT %s %s %s"},
	productenums.BulkVariantSkipped:  {"zh-CN": "跳过 %s 个：%s", "en-US": "EN-VARIANT-SKIP %s %s"},
}

// messageTr 按语言取词（与 shell.TranslateFor 同形：请求期由控制器传入）。
func messageTr(lang string) TranslateFunc {
	return func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
}

// TestVariantSaveMsgFollowsRequestLanguage 合成句整句随请求语言走。
//
// 判据的必要性：合成句由「模板段 + 跳过段前缀 + 逐条原因」多处取词拼成。只要有一处
// 重新写成中文常量（或忘了用传入的 tr），中文环境全绿、英文页面那条回执**静默变回中文** ——
// 不报错、不记日志。
func TestVariantSaveMsgFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(messageInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := messageTr("en-US")

	// 1) 模板段与计数：三个计数都按当前语言填。
	saved := msgText(en, VariantSaveMsg(en, &productdto.SaveVariantListResp{Created: 1, Updated: 2, Deleted: 3}))
	if saved != "EN-VARIANT 1 2 3" {
		t.Errorf("变体清单保存应按当前语言拼装，实际 %q", saved)
	}

	// 2) 跳过段：前缀 + 逐条原因都要按当前语言。
	withSkip := msgText(en, VariantSaveMsg(en, &productdto.SaveVariantListResp{
		Created: 1,
		Skipped: []productdto.VariantSaveSkipResp{{Reason: productenums.VariantSkipHasStock}},
	}))
	if !strings.HasPrefix(withSkip, "EN-VARIANT 1 0 0 EN-VARIANT-SKIP 1 ") {
		t.Errorf("变体清单保存（含跳过）应按当前语言拼装，实际 %q", withSkip)
	}

	// 3) 没有变化：单段模板也走同一条取词路径。
	if noChange := msgText(en, VariantSaveMsg(en, &productdto.SaveVariantListResp{})); noChange != "EN-VARIANT-NOCHANGE" {
		t.Errorf("「没有需要保存的变化」应按当前语言，实际 %q", noChange)
	}

	// 4) 中文请求走中文词条（bulkTextOf 的文本形态）。
	if text := bulkTextOf(messageTr("zh-CN"), variantSaveSaved); text != "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。" {
		t.Errorf("中文请求应取中文词条，实际 %q", text)
	}

	// 5) 词条混进协议外占位符（%d）时回落中文原文，不把 %!d(…) 甩给运营。
	i18n.InjectForTest(map[string]map[string]string{
		productenums.BulkVariantSaved: {"en-US": "EN-BROKEN %d %d %d"},
	}, nil)
	if got := msgText(en, VariantSaveMsg(en, &productdto.SaveVariantListResp{Created: 1})); strings.Contains(got, "%!") {
		t.Errorf("协议外占位符应回落中文原文，实际 %q", got)
	}
}

// msgText 按事实取成品串（与 internal/text.MsgText 同形；测试里不引 gin 那层依赖）。
//
// 词条约定只允许 %s，参数在事实里已经是字符串。
func msgText(tr TranslateFunc, m facing.Msg) string {
	built := tr(m.Key, m.Fallback)
	if len(m.Args) == 0 {
		return built
	}
	args := make([]any, 0, len(m.Args))
	for _, a := range m.Args {
		args = append(args, a)
	}
	return fmt.Sprintf(built, args...)
}
