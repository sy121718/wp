package runtimefragment

// fragment_lang_test.go — 片段语言链路单测（I18N-011，docs/06-D §11）。
//
// 判据（AGENTS.md 测试策略）：这个判断错了会不会静默出错 —— 会。片段语言决定整段
// HTML 用哪套词条、哪套系统页面槽位路径，解析错或传递丢的唯一现象是「页面上的片段
// 变成了另一种语言」，没有任何报错、没有任何日志，只能靠肉眼发现。所以就近单测。

import (
	"context"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// stubProjectLocales 只实现语言清单两个方法的工程契约桩。
//
// 嵌入接口而不是实现全部方法：resolveRequestLang 只消费 EnabledLangs / DefaultLocale，
// 桩里其余方法被调用即是测试自身的缺陷（会 panic 而不是静默通过）。
type stubProjectLocales struct {
	projectcontract.ProjectService
	langs    []string
	def      string
	langsErr error
	defErr   error
}

func (s *stubProjectLocales) EnabledLangs(context.Context, string) ([]string, error) {
	return s.langs, s.langsErr
}

func (s *stubProjectLocales) DefaultLocale(context.Context, string) (string, error) {
	return s.def, s.defErr
}

// TestResolveRequestLang 语言解析链：?lang（需在工程启用清单内）→ 工程默认语言 → i18n 默认。
func TestResolveRequestLang(t *testing.T) {
	orig := i18n.GetDefaultLang()
	defer func() {
		i18n.SetDefaultLang(orig)
		deps.FragmentProject = nil
	}()
	i18n.SetDefaultLang("zh-CN")

	withProject := &stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "en-US"}
	cases := []struct {
		name      string
		project   projectcontract.ProjectService
		projectID string
		raw       string
		want      string
	}{
		{"启用的语言原样返回", withProject, "p1", "zh-CN", "zh-CN"},
		{"未启用的语言回落工程默认", withProject, "p1", "fr-FR", "en-US"},
		{"缺 lang 取工程默认", withProject, "p1", "", "en-US"},
		{"缺 projectID 时不做清单校验（原样透传）", withProject, "", "fr-FR", "fr-FR"},
		{"无契约时原样透传", nil, "p1", "fr-FR", "fr-FR"},
		{"无契约且无 lang 取 i18n 默认", nil, "p1", "", "zh-CN"},
		{"清单查询失败回落工程默认", &stubProjectLocales{langsErr: errStub, def: "en-US"}, "p1", "fr-FR", "en-US"},
		{"默认语言查询失败才用请求值", &stubProjectLocales{defErr: errStub}, "p1", "fr-FR", "fr-FR"},
	}
	for _, tc := range cases {
		deps.FragmentProject = tc.project
		if got := resolveRequestLang(context.Background(), tc.projectID, tc.raw); got != tc.want {
			t.Errorf("%s: 期望 %q 实际 %q", tc.name, tc.want, got)
		}
	}
}

// errStub 一个固定的桩错误（只为让分支走到错误路径）。
var errStub = errStubType{}

type errStubType struct{}

func (errStubType) Error() string { return "语言清单查询失败" }

// TestFragmentEndpointBodyFollowsLang 同一片段在两种语言下响应不同（P5 验收口径）。
//
// 用**测试专用能力**而不是某个业务能力：这条断言的是端点协议（语言解析 → 取词函数），
// 拿业务能力当样本会让它与业务文案的增删一起变红。词条直接注入 i18n 缓存，
// 不建库、不写 seed —— 用例要的是「缓存里有词条」，不是「库里有表」。
func TestFragmentEndpointBodyFollowsLang(t *testing.T) {
	const probe = "langProbeTestOnly"
	Register(Spec{
		Type: probe, Method: "GET", Auth: AuthAnonymous,
		Render: func(_ context.Context, r *Request) (string, error) {
			return "<span>" + r.tr("site.fragment.cart.empty", "购物车是空的") + "</span>", nil
		},
	})
	i18n.InjectForTest(map[string]map[string]string{
		"site.fragment.cart.empty": {"zh-CN": "购物车是空的", "en-US": "Your cart is empty"},
	}, nil)
	defer i18n.InjectForTest(map[string]map[string]string{}, nil)
	deps.FragmentProject = &stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "zh-CN"}
	defer func() { deps.FragmentProject = nil }()

	r := newRouter()
	en := doGet(t, r, "/_fragments/"+probe+"?projectId=p1&lang=en-US")
	if !strings.Contains(en.Body.String(), "Your cart is empty") {
		t.Fatalf("en-US 请求应出英文词条，实际 %s", en.Body.String())
	}
	if strings.Contains(en.Body.String(), "购物车是空的") {
		t.Fatalf("en-US 请求不该出中文词条，实际 %s", en.Body.String())
	}
	// 缺 lang：回落工程默认语言（zh-CN）—— 这正是产物没带 ?lang 时的现实形态，
	// 也是 Content-Language 头存在的意义（片段语言可能与页面语言不一致）。
	def := doGet(t, r, "/_fragments/"+probe+"?projectId=p1")
	if !strings.Contains(def.Body.String(), "购物车是空的") {
		t.Fatalf("缺 lang 应回落工程默认语言，实际 %s", def.Body.String())
	}
	if got := def.Header().Get("Content-Language"); got != "zh-CN" {
		t.Fatalf("缺 lang 时 Content-Language 应为工程默认语言，实际 %q", got)
	}
}

// TestOrderFragmentURLEscapesLang 订单片段的内部链接对 lang 做 URL 转义。
//
// lang 是访客可改的 query：手拼时一个 & 就能把后面的参数顶掉（projectId 被换掉
// 即等于让访客自选工程）。它不是越权面（片段只出公开/自有数据），但会让链接本身
// 变成能把 URL 改坏的注入点 —— 转义后它只是 URL 里的一个普通取值。
func TestOrderFragmentURLEscapesLang(t *testing.T) {
	r := &Request{Params: map[string]string{fragmentLangParam: "en-US&projectId=evil"}}
	list := orderListFragmentURL(r, "proj-1", "paid", 0, 0)
	if !strings.Contains(list, "lang=en-US%26projectId%3Devil") {
		t.Fatalf("ordersList 的 lang 应被转义，实际 %s", list)
	}
	if strings.Contains(list, "&projectId=evil") {
		t.Fatalf("ordersList 的 lang 不得顶掉后面的参数，实际 %s", list)
	}
	detail := orderDetailURL(r, "proj-1", 7)
	if !strings.Contains(detail, "lang=en-US%26projectId%3Devil") {
		t.Fatalf("orderDetail 的 lang 应被转义，实际 %s", detail)
	}
}
