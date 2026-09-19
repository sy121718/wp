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
		SetFragmentProject(nil)
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
		SetFragmentProject(tc.project)
		if got := resolveRequestLang(context.Background(), tc.projectID, tc.raw); got != tc.want {
			t.Errorf("%s: 期望 %q 实际 %q", tc.name, tc.want, got)
		}
	}
}

// errStub 一个固定的桩错误（只为让分支走到错误路径）。
var errStub = errStubType{}

type errStubType struct{}

func (errStubType) Error() string { return "语言清单查询失败" }

// TestRenderProductListKeepsLangInPager 翻页/换筛选链接保留请求语言。
//
// 组件的翻页链接由 pushQuery（片段层灌入的语义参数串）拼装：串里没有 lang，
// 「英文站点翻到第 2 页」的请求就不再带语言，片段回落工程默认语言 ——
// 现象是「一翻页列表自己变回中文」，而两次请求都是 200、都渲染成功。
func TestRenderProductListKeepsLangInPager(t *testing.T) {
	defer SetCollectionResolver(nil)
	SetCollectionResolver(&stubPager{total: 50})

	out, err := renderProductList(context.Background(), &Request{
		Type: "productList",
		Lang: "en-US",
		Params: map[string]string{
			"nodeId": "list-1", "projectId": "proj-1",
			"titleField": "item.name", "linkField": "item.slug", "linkPrefix": "/products/",
			"pageSize": "4", "page": "2", "lang": "en-US",
		},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "lang=en-US") {
		t.Fatalf("翻页链接应保留请求语言（否则翻页后片段回落默认语言）\n%s", out)
	}

	// 反向：请求没带 lang 时不得凭空塞语言进链接 —— 语言由 URL 显式表达，
	// 不靠片段层替访客「补」一个（那会让默认语言与显式语言两条路径混起来）。
	outNoLang, err := renderProductList(context.Background(), &Request{
		Type: "productList",
		Params: map[string]string{
			"nodeId": "list-1", "projectId": "proj-1",
			"titleField": "item.name", "linkField": "item.slug", "linkPrefix": "/products/",
			"pageSize": "4", "page": "2",
		},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(outNoLang, "lang=") {
		t.Fatalf("请求未带 lang 时链接里不该出现语言参数\n%s", outNoLang)
	}
}

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
	SetFragmentProject(&stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "zh-CN"})
	defer SetFragmentProject(nil)

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

// TestLoginPanelFollowsLang loginPanel 的文案按请求语言取词（迁移 293）。
//
// 它曾是本包唯一恒为默认语言的片段文案（两句中文写死在 capability.go 里）：
// 端点已解析出语言、其余片段都已走 r.tr，只有它不看语言 ——
// 现象是「英文站点的登录面板显示中文」，没有任何报错。
func TestLoginPanelFollowsLang(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"site.fragment.login_panel.login_register":    {"zh-CN": "登录 / 注册", "en-US": "Sign in / Sign up"},
		"site.fragment.login_panel.continue_shopping": {"zh-CN": "继续购物", "en-US": "Continue shopping"},
	}, nil)
	defer i18n.InjectForTest(map[string]map[string]string{}, nil)
	SetFragmentProject(&stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "zh-CN"})
	defer SetFragmentProject(nil)

	r := newRouter()
	en := doGet(t, r, "/_fragments/loginPanel?projectId=p1&lang=en-US")
	if !strings.Contains(en.Body.String(), "Sign in / Sign up") {
		t.Fatalf("en-US 请求应出英文文案，实际 %s", en.Body.String())
	}
	// 语义上下文影响取的是哪一条词条，不影响语言链路。
	enCtx := doGet(t, r, "/_fragments/loginPanel?projectId=p1&lang=en-US&context=visitorSession")
	if !strings.Contains(enCtx.Body.String(), "Continue shopping") {
		t.Fatalf("visitorSession + en-US 应出英文会话态文案，实际 %s", enCtx.Body.String())
	}
	zh := doGet(t, r, "/_fragments/loginPanel?projectId=p1")
	if !strings.Contains(zh.Body.String(), "登录 / 注册") {
		t.Fatalf("缺 lang 应回落工程默认语言（zh-CN），实际 %s", zh.Body.String())
	}
}

// TestLoginPanelFallsBackToSourceText 词条缺失时回退 Go 里的中文原文，绝不输出裸 key。
//
// 与构建期组件同一口径：访客看到 site.fragment.login_panel.login_register 是事故，
// 看到中文原文只是「这个语言还没翻译」。
func TestLoginPanelFallsBackToSourceText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{}, nil)
	defer i18n.InjectForTest(map[string]map[string]string{}, nil)
	SetFragmentProject(&stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "zh-CN"})
	defer SetFragmentProject(nil)

	w := doGet(t, newRouter(), "/_fragments/loginPanel?projectId=p1&lang=en-US")
	body := w.Body.String()
	if strings.Contains(body, "site.fragment.login_panel") {
		t.Fatalf("词条缺失时不得输出裸 key，实际 %s", body)
	}
	if !strings.Contains(body, "登录 / 注册") {
		t.Fatalf("词条缺失时应回退中文原文，实际 %s", body)
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

// TestRenderProductListSetsLangInRenderContext 片段期渲染上下文带上本次请求解析出的语言。
//
// 组件的**实例配置**（容器那条 hx-get）在构建期由 RenderContext.Lang 拼出 lang；
// 片段期走同一个 fragmentQuery —— 这里若不把 r.Lang 传进 RenderContext，
// 重渲染出来的容器就不带 lang，于是「构建期产物带 lang、片段刷新后的容器不带」，
// 同一个语言维度走了两条路径：容器再触发一次 load 就回落工程默认语言。
// 断言只看容器那条 URL（不看整段 HTML），免得被翻页链接里的 lang 蒙混过去。
func TestRenderProductListSetsLangInRenderContext(t *testing.T) {
	defer SetCollectionResolver(nil)
	SetCollectionResolver(&stubPager{total: 50})

	out, err := renderProductList(context.Background(), &Request{
		Type: "productList",
		Lang: "en-US",
		Params: map[string]string{
			"nodeId": "list-1", "projectId": "proj-1",
			"titleField": "item.name", "linkField": "item.slug", "linkPrefix": "/products/",
			"pageSize": "4", "page": "2",
		},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	const marker = `hx-get="/_fragments/productList?`
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("未找到容器的片段地址\n%s", out)
	}
	rest := out[i+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("容器片段地址未闭合\n%s", out)
	}
	if q := rest[:end]; !strings.Contains(q, "lang=en-US") {
		t.Fatalf("容器实例配置应带请求语言（RenderContext.Lang 没传），实际 %q", q)
	}
}
