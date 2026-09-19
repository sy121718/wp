// product_translation_target_lang_test.go — 商品多语言工作台的「目标语言」口径（issue #12 后续）。
//
// 本文件锁住两条产品决策（它们是同一个现场的两半：站点只启用了 zh-CN 时，下拉只有一项、
// 看起来像「只能设置中文」，而且默认选中的那一项就是源语言，等于没得翻）：
//  1. 默认选中的目标语言**不是**站点源语言（源语言 = 站点默认语言，见 handler 的 sourceLangOf）；
//  2. 启用语言里只剩源语言一种时给**可行动空态**：说明为什么没有目标语言 + 指向站点设置
//     的语言清单入口，而不是渲染一个「默认选中源语言」的表单。
//
// PG 不可用时 t.Skip（与其他 feature 测试一致）。
package feature

import (
	"context"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

// trLangSelect 截出本页的目标语言下拉。
//
// 必须按 id 截取：后台外壳自己还有一个界面语言选择器（layout.html 的 lang 下拉，
// 里面同样有 <option value="zh-CN" selected>简体中文），整页 Contains 会把它一起算进来。
func trLangSelect(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `id="tr-lang"`)
	if i < 0 {
		t.Fatalf("页面没有目标语言下拉：%s", body)
	}
	rest := body[i:]
	if j := strings.Index(rest, "</select>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestProductTranslationWorkbenchDefaultsToNonSourceTargetLang 目标语言下拉只列非源语言，
// 且默认选中第一个非源语言的启用语言（fixture：默认 zh-CN + 启用 en-US）。
func TestProductTranslationWorkbenchDefaultsToNonSourceTargetLang(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	engine := trWorkbench(t, f)

	// 显式请求源语言：不允许把源语言当成目标语言选中。
	sel := trLangSelect(t, trWorkbenchGet(t, engine, f.projectID, "", "zh-CN"))
	if strings.Contains(sel, `value="zh-CN"`) {
		t.Fatalf("源语言（zh-CN）不应出现在目标语言下拉里：%s", sel)
	}
	if !strings.Contains(sel, `value="en-US" selected`) {
		t.Fatalf("默认应选中第一个非源语言的启用语言（en-US）：%s", sel)
	}
}

// TestProductTranslationWorkbenchEmptyStateWhenOnlySourceLang 启用语言只有源语言一种时，
// 页面给可行动空态：说明原因 + 语言清单入口，且不渲染语言下拉与保存表单。
func TestProductTranslationWorkbenchEmptyStateWhenOnlySourceLang(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 另建一个只启用默认语言的工程 —— 这正是「下拉只有一项」的现场。
	solo, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "单语言工程"})
	if err != nil {
		t.Fatalf("创建单语言工程失败: %v", err)
	}
	enabled := true
	if _, err := f.projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: solo.ID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", SortOrder: 1, IsDefault: true, Enabled: &enabled},
		},
	}); err != nil {
		t.Fatalf("初始化单语言清单失败: %v", err)
	}

	engine := trWorkbench(t, f)
	body := trWorkbenchGet(t, engine, solo.ID, "", "")
	for _, want := range []string{
		"商品多语言",           // 页头仍在：没有目标语言不是渲染中断
		"没有可翻译的目标语言",      // 空态标题（t 缺词条时走中文兜底）
		"没有第二种语言可以作为翻译目标", // 空态说明：为什么翻不了
		"zh-CN",                              // 点出源语言就是当前唯一的启用语言
		"去站点设置启用更多语言",                        // 可行动出口
		"/admin/settings?project=" + solo.ID, // 且指向本工程的语言清单
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("空态缺少 %q：%s", want, body)
		}
	}
	if strings.Contains(body, "没有可翻译文本") {
		t.Fatalf("只剩源语言时应给「没有目标语言」空态，而不是「没有可翻译文本」：%s", body)
	}
	if strings.Contains(body, `id="tr-lang"`) {
		t.Fatalf("没有目标语言时不应渲染语言下拉：%s", body)
	}
	if strings.Contains(body, "保存全部") {
		t.Fatalf("没有目标语言时不应渲染保存表单：%s", body)
	}
}
