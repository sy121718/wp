package navigationhttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
)

type workbenchNavigationStub struct {
	navigationcontract.NavigationService
	index navigationcontract.SourceIndex
	err   error

	opened []string
}

func (s *workbenchNavigationStub) OpenSourceIndex(_ context.Context, projectID string) (navigationcontract.SourceIndex, error) {
	s.opened = append(s.opened, projectID)
	if s.err != nil {
		return nil, s.err
	}
	return s.index, nil
}

type workbenchIndex struct {
	entries []navigationcontract.TranslationSourceEntry
	failed  []navigationcontract.KindReadError
	kinds   []string
}

func (i *workbenchIndex) Locate(hash string) (navigationcontract.TranslationSourceEntry, bool) {
	for _, e := range i.entries {
		if e.Hash == hash {
			return e, true
		}
	}
	return navigationcontract.TranslationSourceEntry{}, false
}

func (i *workbenchIndex) Entries() []navigationcontract.TranslationSourceEntry {
	return append([]navigationcontract.TranslationSourceEntry{}, i.entries...)
}

func (i *workbenchIndex) Kinds() []string { return append([]string{}, i.kinds...) }

func (i *workbenchIndex) KindErrors() []navigationcontract.KindReadError {
	return append([]navigationcontract.KindReadError{}, i.failed...)
}

func (i *workbenchIndex) Require() error { return nil }

func injectWorkbenchKindEntries(t *testing.T) {
	t.Helper()
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.KindHeader: {
			"zh-CN": "页眉导航", "en-US": "Header navigation",
		},
		navigationenums.KindHeaderMobile: {
			"zh-CN": "页眉导航（移动端）", "en-US": "Header navigation (mobile)",
		},
		navigationenums.KindFooter: {
			"zh-CN": "页脚导航", "en-US": "Footer navigation",
		},
		navigationenums.KindFooterMobile: {
			"zh-CN": "页脚导航（移动端）", "en-US": "Footer navigation (mobile)",
		},
		// 逐位置读取失败的错误事实也要有词条：声明处只给 key + 状态码，
		// 成品句归词库。不注入的话 tr(key, key) 会把裸 key 渲染上屏。
		navigationenums.ErrSourceKindFailed: {
			"zh-CN": workbenchKindFailedText, "en-US": "This position could not be read",
		},
	})
}

// workbenchKindFailedText 是「某位置读取失败」的对外文案（测试侧与词库对齐的一份）。
const workbenchKindFailedText = "该位置读取失败，其余位置照常展示"

func newWorkbenchRouter(t *testing.T, nav navigationcontract.NavigationService) http.Handler {
	t.Helper()
	injectWorkbenchKindEntries(t)
	router := newNavigationsPageTestRouter(t, nav, &pagePresentationProjectStub{})
	router.GET("/admin/navigations/translations",
		pageview.Slot(NavigationTranslationsTemplate, pageview.RenderPage),
		NewNavigationTranslationHandle(nav, &pagePresentationProjectStub{}).NavigationTranslations)
	return router
}

func renderWorkbenchBody(t *testing.T, nav navigationcontract.NavigationService, query string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	newWorkbenchRouter(t, nav).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/navigations/translations"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("译文工作台渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestTranslationWorkbenchGroupsAllFourKinds(t *testing.T) {
	shared := i18n.ContentHash("共用菜单")
	nav := &workbenchNavigationStub{
		index: &workbenchIndex{
			kinds: []string{"header", "header_mobile", "footer", "footer_mobile"},
			entries: []navigationcontract.TranslationSourceEntry{
				{
					Hash: shared, Source: "共用菜单", Path: "/shared", NodeID: "n-1",
					Context: "navigation.label",

					Kinds: []string{"header", "footer_mobile"},
				},
			},
		},
	}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=en-US")

	for _, want := range []string{"Header navigation", "Header navigation (mobile)", "Footer navigation", "Footer navigation (mobile)"} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台缺少位置分组 %q（四个位置必须各自成组）", want)
		}
	}

	if strings.Contains(body, "admin.navigations.kind.") {
		t.Errorf("页面上出现了位置 key 字面量（模板没有取词）：%s", body)
	}

	if got := strings.Count(body, ">共用菜单<"); got != 1 {
		t.Errorf("同一段原文应只列一行，实得 %d 行", got)
	}

	for _, want := range []string{
		`class="field nav-tr-row"`,
		`name="context" value="navigation.label"`,
		`name="hash" value="` + shared + `"`,
		`name="target"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台缺少 %q", want)
		}
	}

	for _, want := range []string{
		`hx-post="/api/navigation/translations"`,
		`data-write-builder="navigation-translations"`,
		`data-write-result`,
		`name="action" value="translations"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台缺少写契约声明 %q", want)
		}
	}

	if !rowInsideSection(body, "Header navigation", shared) {
		t.Errorf("原文应落在「Header navigation」这一组内（跨位置条目落在规范顺序最靠前的位置）")
	}
	if rowInsideSection(body, "Footer navigation", shared) {
		t.Errorf("原文不应落进「Footer navigation」组：分组索引错位会把行塞进末组")
	}
	if rowInsideSection(body, "Header navigation (mobile)", shared) {
		t.Errorf("原文不应落进「Header navigation (mobile)」组（那是移动端位置，本行不属于它）")
	}
	if rowInsideSection(body, "Footer navigation (mobile)", shared) {
		t.Errorf("原文不应落进「Footer navigation (mobile)」组（共用位置不改变行的归属）")
	}

	if len(nav.opened) != 1 || nav.opened[0] != "project-1" {
		t.Errorf("来源索引必须只按当前工程打开一次，实得 %v", nav.opened)
	}
}

func rowInsideSection(body, title, hash string) bool {
	head := strings.Index(body, ">"+title+"<")
	if head < 0 {
		return false
	}
	rest := body[head:]
	end := len(rest)
	for _, other := range []string{
		">Header navigation<", ">Header navigation (mobile)<",
		">Footer navigation<", ">Footer navigation (mobile)<",
	} {
		if i := strings.Index(rest, other); i > 0 && i < end {
			end = i
		}
	}
	return strings.Contains(rest[:end], `name="hash" value="`+hash+`"`)
}

func TestTranslationWorkbenchDropsUntranslatableRows(t *testing.T) {
	nav := &workbenchNavigationStub{
		index: &workbenchIndex{
			kinds: []string{"header"},
			entries: []navigationcontract.TranslationSourceEntry{
				{Hash: i18n.ContentHash("123"), Source: "123", Context: "navigation.label", Kinds: []string{"header"}},
				{Hash: i18n.ContentHash("—"), Source: "—", Context: "navigation.label", Kinds: []string{"header"}},
				{Hash: i18n.ContentHash("首页"), Source: "首页", Context: "navigation.label", Kinds: []string{"header"}},
			},
		},
	}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=zh-CN")
	if strings.Contains(body, `name="hash" value="`+i18n.ContentHash("123")+`"`) {
		t.Error("纯数字原文不应出现在工作台（不可翻译行必须被读侧过滤）")
	}
	if strings.Contains(body, `name="hash" value="`+i18n.ContentHash("—")+`"`) {
		t.Error("纯符号原文不应出现在工作台")
	}
	if !strings.Contains(body, `name="hash" value="`+i18n.ContentHash("首页")+`"`) {
		t.Error("可翻译的原文必须出现在工作台")
	}
}

func TestTranslationWorkbenchDegradesPerKind(t *testing.T) {
	ok := i18n.ContentHash("正常菜单")
	nav := &workbenchNavigationStub{
		index: &workbenchIndex{
			kinds:  []string{"header", "footer"},
			failed: []navigationcontract.KindReadError{{Kind: "footer", Err: navigationenums.ErrSourceKindDetail("footer")}},
			entries: []navigationcontract.TranslationSourceEntry{
				{Hash: ok, Source: "正常菜单", Context: "navigation.label", Kinds: []string{"header"}},
			},
		},
	}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=zh-CN")

	if !strings.Contains(body, `name="hash" value="`+ok+`"`) {
		t.Error("可读位置的行必须照常展示（逐位置降级，不是整页失败）")
	}

	if !strings.Contains(body, workbenchKindFailedText) {
		t.Errorf("失败位置应展示该位置的错误事实（词库译文）：%s", body)
	}
	if strings.Contains(body, navigationenums.ErrSourceKindFailed) {
		t.Errorf("失败位置不得把裸 key 渲染上屏：%s", body)
	}
	if got := strings.Count(body, "该位置还没有菜单项"); got != 2 {

		t.Errorf("失败位置不得渲染成空菜单，空态文案数量实得 %d（期望 2）", got)
	}

	if strings.Contains(body, "没有可翻译的菜单文字") {
		t.Error("有位置读失败时不得进整页空态")
	}
}

func TestTranslationWorkbenchIndexFailureIsPageError(t *testing.T) {
	nav := &workbenchNavigationStub{err: errors.New("boom: SQLSTATE 42P01 navigations")}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=zh-CN")
	// 索引整页打不开走 navigationLoadErrorOf → 业务事实 / 归口句，两条形态都接受。
	if !strings.Contains(body, navigationInternalFallback) &&
		!strings.Contains(body, "操作失败，请稍后重试") {
		t.Errorf("索引打开失败必须给出错误事实：%s", body)
	}
	for _, leak := range []string{"SQLSTATE", "42P01", "boom"} {
		if strings.Contains(body, leak) {
			t.Errorf("页面透出了内部原文 %q", leak)
		}
	}
	if strings.Contains(body, `name="hash"`) {
		t.Error("索引打不开时不应渲染任何可提交行")
	}
}

type workbenchNoIndexStub struct {
	navigationcontract.NavigationService
}

func (s *workbenchNoIndexStub) Tree(_ context.Context, _, kind string) ([]*navigationdto.NavigationNode, error) {

	return []*navigationdto.NavigationNode{{ID: "legacy", Title: "旧读法菜单", Path: "/legacy"}}, nil
}

func TestTranslationWorkbenchWithoutSourceIndexKeepsEmptyState(t *testing.T) {
	body := renderWorkbenchBody(t, &workbenchNoIndexStub{}, "?project=project-1&lang=zh-CN")
	if strings.Contains(body, "旧读法菜单") {
		t.Error("缺窄接口时不得回退到旧 Tree 读法（会把「没接上」伪装成「有数据」）")
	}
	if !strings.Contains(body, "没有可翻译的菜单文字") {
		t.Errorf("缺窄接口时应停在空态：%s", body)
	}
}
