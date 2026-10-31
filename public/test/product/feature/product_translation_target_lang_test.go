package feature

import (
	"context"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

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

func TestProductTranslationWorkbenchDefaultsToNonSourceTargetLang(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	engine := trWorkbench(t, f)

	sel := trLangSelect(t, trWorkbenchGet(t, engine, f.projectID, "", "zh-CN"))
	if strings.Contains(sel, `value="zh-CN"`) {
		t.Fatalf("源语言（zh-CN）不应出现在目标语言下拉里：%s", sel)
	}
	if !strings.Contains(sel, `value="en-US" selected`) {
		t.Fatalf("默认应选中第一个非源语言的启用语言（en-US）：%s", sel)
	}
}

func TestProductTranslationWorkbenchEmptyStateWhenOnlySourceLang(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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
		"商品多语言",
		"没有可翻译的目标语言",
		"没有第二种语言可以作为翻译目标",
		"zh-CN",
		"去站点设置启用更多语言",
		"/admin/settings?project=" + solo.ID,
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
