package navigationhttp

// navigation_translation_workbench_test.go — 译文工作台**读侧**的落点契约。
//
// 被守的三条边界（AGENTS.md：Go 只出事实、模板取词、inbound 无第二段业务）：
//
//  1. 分组标题不是 Go 取的词，而是 key + 模板取词 —— 与导航菜单页的 kind 下拉同源，
//     并且**四个位置各自一组**（移动端菜单此前根本不可翻译，桌面与移动端却是两份数据）；
//  2. 读失败逐位置降级：某位置读不到时该组给错误槽，其余组照常展示（不是整页失败，
//     也不是静默把它渲染成「这里没有菜单项」）；
//  3. 保存契约一条不少（create 档 / 行数组 / 回跳 / 空提交不可翻译行），
//     现有译文按**工程优先、全局回落**读取。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
)

// workbenchNavigationStub 只实现工作台用到的两件事：共享来源索引与窄接口断言。
//
// 嵌入接口（nil）而不是逐方法实现：本页只消费 OpenSourceIndex，其余方法不该在测试里
// 编造一份「看起来能用」的假实现。
type workbenchNavigationStub struct {
	navigationcontract.NavigationService
	index navigationcontract.SourceIndex
	err   error
	// opened 记录被打开的工程 id（断言工程隔离：所有读都必须带当前工程）。
	opened []string
}

func (s *workbenchNavigationStub) OpenSourceIndex(_ context.Context, projectID string) (navigationcontract.SourceIndex, error) {
	s.opened = append(s.opened, projectID)
	if s.err != nil {
		return nil, s.err
	}
	return s.index, nil
}

// workbenchIndex 手写一份索引：读侧只消费接口，不需要真的跑一遍来源收集。
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

// workbenchKindEntries 四个位置标题的词条（**按线上 seed 的英文原文**合成）。
//
// 为什么要在这里注入：本文件断言的是「模板按 key 取词并且四个位置各自成组」，
// 而取词结果取决于库里的词条 —— 单元测试没有库，测试进程里的 i18n 缓存是空的。
// 不注入时 t() 只能回落到模板里的中文兜底（"页眉导航"），于是「四个位置在英文界面下
// 各自成组」这条事实**根本不可能被观察到**：断言会一直红，而与代码对不对无关。
//
// 注入的是线上词条的副本（迁移 192 / 290 的 en-US 原文），不是自造的哨兵值 ——
// 用哨兵只能证明「key 到了取词层」，用真实原文还顺带钉住了「哪个 key 该出哪句话」。
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
	})
}

// newWorkbenchRouter 与线上同一条渲染路径（页壳 + 真实 Jet 模板 + 请求语言取词）。
func newWorkbenchRouter(t *testing.T, nav navigationcontract.NavigationService) http.Handler {
	t.Helper()
	injectWorkbenchKindEntries(t)
	router := newNavigationsPageTestRouter(t, nav, &pagePresentationProjectStub{})
	router.GET("/admin/navigations/translations",
		adminui.PageSlot(NavigationTranslationsTemplate),
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

// TestTranslationWorkbenchGroupsAllFourKinds 四个位置各自成组，标题由模板取词。
func TestTranslationWorkbenchGroupsAllFourKinds(t *testing.T) {
	shared := i18n.ContentHash("共用菜单")
	nav := &workbenchNavigationStub{
		index: &workbenchIndex{
			kinds: []string{"header", "header_mobile", "footer", "footer_mobile"},
			entries: []navigationcontract.TranslationSourceEntry{
				{
					Hash: shared, Source: "共用菜单", Path: "/shared", NodeID: "n-1",
					Context: "navigation.label",
					// 同一段原文同时出现在页眉与页脚移动端：只列一行（落在规范顺序最靠前的位置），
					// 但两个位置都要能被失效派发命中。
					Kinds: []string{"header", "footer_mobile"},
				},
			},
		},
	}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=en-US")

	// 四个位置标题都在（英文词条，来自 adminui.Prepare 注入的取词函数）。
	for _, want := range []string{"Header navigation", "Header navigation (mobile)", "Footer navigation", "Footer navigation (mobile)"} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台缺少位置分组 %q（四个位置必须各自成组）", want)
		}
	}
	// 没有任何位置抛过 key 到页面上（模板取了词，不是把 key 原样渲染）。
	if strings.Contains(body, "admin.navigations.kind.") {
		t.Errorf("页面上出现了位置 key 字面量（模板没有取词）：%s", body)
	}
	// 同一段文字只列一行，且落在页眉组里。
	if got := strings.Count(body, ">共用菜单<"); got != 1 {
		t.Errorf("同一段原文应只列一行，实得 %d 行", got)
	}
	// 行容器与三个具名字段（保存契约的渲染形态）。
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
	// 保存契约一条不少。
	for _, want := range []string{
		`data-write-url="/api/navigation/update"`,
		`data-write="create"`,
		`data-write-rows=".nav-tr-row;context,hash,target"`,
		`data-write-items`,
		`data-write-nav="/admin/navigations/translations?`,
		`name="action" value="translations"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台缺少写契约声明 %q", want)
		}
	}
	// 行必须落在**自己的位置分组**里，不是全部堆进最后一个组。
	//
	// 这条与上面的「四个标题都在」互补：分组标题遍历的是位置列表，即使行的位置索引全错
	// 也照样四个标题齐全。判据用「标题 → 下一个标题之前」这一段里有没有该行。
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
	// 工程隔离：索引只按当前工程打开一次。
	if len(nav.opened) != 1 || nav.opened[0] != "project-1" {
		t.Errorf("来源索引必须只按当前工程打开一次，实得 %v", nav.opened)
	}
}

// rowInsideSection 页面上「某个位置标题」这一节里是否包含给定原文指纹的隐藏域。
//
// 节的范围 = 该标题之后、下一个位置标题之前。模板把每个位置渲染成一张 card，
// 标题与行按同一顺序输出，所以「行出现在标题之后、下一个标题之前」即「行属于该组」。
// 找不到标题时返回 false（标题缺失由别的断言负责，这里不重复报）。
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

// TestTranslationWorkbenchDropsUntranslatableRows 不可翻译的原文不占展示行。
//
// 索引刻意不提前丢行（写侧要能给出「这段本来就不翻译」的准确原因），过滤是读侧的责任：
// 纯数字 / 纯符号 / 空串在页面上没有可填的译文。
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

// TestTranslationWorkbenchDegradesPerKind 某位置读失败：该组给错误槽，其余组照常。
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
	// 可读位置照常展示。
	if !strings.Contains(body, `name="hash" value="`+ok+`"`) {
		t.Error("可读位置的行必须照常展示（逐位置降级，不是整页失败）")
	}
	// 失败位置给错误事实，而不是「该位置还没有菜单项」。
	if !strings.Contains(body, navigationenums.FallbackFor(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("失败位置应展示该位置的错误事实：%s", body)
	}
	if got := strings.Count(body, "该位置还没有菜单项"); got != 2 {
		// 页眉有行不出空态；页眉移动端、页脚移动端无行 → 两处空态；页脚是失败组，
		// **不得**再显示空态文案（「读不到」与「这里没有菜单」是两件事）。
		t.Errorf("失败位置不得渲染成空菜单，空态文案数量实得 %d（期望 2）", got)
	}
	// 整页空态不得抢走失败事实：它会把所有分组一起替换掉。
	if strings.Contains(body, "没有可翻译的菜单文字") {
		t.Error("有位置读失败时不得进整页空态")
	}
}

// TestTranslationWorkbenchIndexFailureIsPageError 索引整体打开失败：给一条归口错误，不伪装成空态。
func TestTranslationWorkbenchIndexFailureIsPageError(t *testing.T) {
	nav := &workbenchNavigationStub{err: errors.New("boom: SQLSTATE 42P01 navigations")}
	body := renderWorkbenchBody(t, nav, "?project=project-1&lang=zh-CN")
	if !strings.Contains(body, navigationenums.FallbackFor(navigationenums.ErrSourceKindFailed)) &&
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

// TestTranslationWorkbenchWithoutSourceIndexKeepsEmptyState 老实现（无窄接口）不静默回退旧读法。
//
// 它是「没接上」而不是「这里没有菜单」：页面退化为空态，但绝不回退到按 kind 拉 Tree 的
// 旧读法 —— 两份来源并存，症状是「同一个工作台有时能看到移动端菜单、有时看不到」。
type workbenchNoIndexStub struct {
	navigationcontract.NavigationService
}

func (s *workbenchNoIndexStub) Tree(_ context.Context, _, kind string) ([]*navigationdto.NavigationNode, error) {
	// 旧读法若被调用会拿到非空树：断言它没有被调用的唯一办法就是「页面没有行」。
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
