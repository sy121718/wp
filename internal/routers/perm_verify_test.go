package routers

import (
	"context"
	"errors"
	"strings"
	"testing"

	aicontract "go_wp/internal/module/ai/contract"
	"go_wp/pkg/mcp"
	mw "go_wp/pkg/middleware"
)

// fakePermDirectory 是权限目录的最小只读端口：校验逻辑只依赖 List。
type fakePermDirectory struct {
	entries map[string]aicontract.PermEntry
	err     error
}

func (d fakePermDirectory) Exists(context.Context, string) (bool, error) {
	_, ok := d.entries[""]
	return ok, d.err
}

// ListByPathMethod 按 api_path/api_method 查目录行（含被停用的），供空 Perm 入口核对。
func (d fakePermDirectory) ListByPathMethod(_ context.Context, path, method string) ([]aicontract.PermEntry, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := make([]aicontract.PermEntry, 0, 1)
	for _, e := range d.entries {
		if e.Path == path && strings.EqualFold(e.Method, method) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (d fakePermDirectory) List(_ context.Context, codes []string) ([]aicontract.PermEntry, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := make([]aicontract.PermEntry, 0, len(codes))
	for _, code := range codes {
		if e, ok := d.entries[code]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func registryWith(t *testing.T, specs ...mw.RouteSpec) *mw.Registry {
	t.Helper()
	reg := mw.NewRegistry()
	for _, spec := range specs {
		reg.DeclareRoute(spec)
	}
	return reg
}

// 目录里缺失、或路径/方法与注册事实不一致，都必须列入问题清单。
//
// 页面（RoutePage）的目录行记的是**被复用的操作**，不是后台页面路径：
// 拿 /admin/pages 去比 /api/page/list 会把一整批合法页面报成漂移（真实装配里 34 条）。
func TestVerifyDeclaredPermissionsReportsMismatch(t *testing.T) {
	reg := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected},
		mw.RouteSpec{Method: "GET", Path: "/admin/pages", Perm: "page:read", Kind: mw.RoutePage,
			AuthObject: "/api/page/list", AuthMethod: "GET"},
		mw.RouteSpec{Method: "POST", Path: "/api/missing", Perm: "m:write", Kind: mw.RouteProtected},
	)
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write":   {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"page:read": {Code: "page:read", Path: "/api/page/list", Method: "GET", Status: 1},
	}}
	problems := verifyDeclaredPermissions(context.Background(), reg, dir)
	if len(problems) != 1 || !strings.Contains(problems[0], "m:write") {
		t.Fatalf("应只报缺失的 m:write，实际：%+v", problems)
	}

	// 反例一：页面复用的对象与目录记的不一致 → 必须报。
	mismatchObject := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write":   {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"page:read": {Code: "page:read", Path: "/api/other/list", Method: "GET", Status: 1},
	}}
	problems = verifyDeclaredPermissions(context.Background(), reg, mismatchObject)
	if len(problems) != 2 {
		t.Fatalf("缺 m:write 与页面对象不一致时必须两条都报，实际：%+v", problems)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "/api/page/list") {
		t.Fatalf("必须点名页面实际检查的对象 /api/page/list，实际：%+v", problems)
	}

	// 反例二：页面复用的是写操作（POST），目录却按 GET 记 → 必须报。
	mismatchMethod := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write":   {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"page:read": {Code: "page:read", Path: "/api/page/list", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, mismatchMethod); len(problems) != 2 {
		t.Fatalf("页面鉴权方法与目录不一致时必须报，实际：%+v", problems)
	}
}

// 有独立权限点的页面：目录的 api_path 记**真实入口**。
//
// 反例就是第十一轮浏览器验收抓到的线上缺陷：/api/publication/seo-audit 额外检查
// /api/seo/audit，目录记的却是前者；若按 AuthObject 比对，实例会永远 not_ready。
func TestVerifyDeclaredPermissionsComparesRealEntryForOwnPerm(t *testing.T) {
	reg := registryWith(t, mw.RouteSpec{
		Method: "POST", Path: "/api/publication/seo-audit", Perm: "publication:seo_audit",
		Kind: mw.RouteProtected, AuthObject: "/api/seo/audit", AuthMethod: "POST",
	})
	// 真实入口与额外的 SEO 鉴权对象各有一行 —— 这才叫「核对完整」。
	good := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"publication:seo_audit": {Code: "publication:seo_audit", Path: "/api/publication/seo-audit", Method: "POST", Status: 1},
		"seo:audit":             {Code: "seo:audit", Path: "/api/seo/audit", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, good); len(problems) != 0 {
		t.Fatalf("真实入口与额外对象都在目录里时不应报问题，实际：%+v", problems)
	}
	// 只有自身行、没有额外对象行：入口运行期会永远被拒，必须启动就报。
	onlySelf := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"publication:seo_audit": {Code: "publication:seo_audit", Path: "/api/publication/seo-audit", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, onlySelf); len(problems) != 1 ||
		!strings.Contains(problems[0], "/api/seo/audit") {
		t.Fatalf("缺额外鉴权对象行时必须点名报出，实际：%+v", problems)
	}
	bad := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"publication:seo_audit": {Code: "publication:seo_audit", Path: "/api/seo/audit", Method: "POST", Status: 1},
		"seo:audit":             {Code: "seo:audit", Path: "/api/seo/audit", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, bad); len(problems) != 1 {
		t.Fatalf("目录记的是复用对象而非真实入口时应报不一致，实际：%+v", problems)
	}
}

// 空 Perm 的页面/写入口不是「不查权限」，而是复用别的鉴权对象：
// 必须去目录里确认那条策略行真的存在。
func TestVerifyDeclaredPermissionsChecksReusedObjectForEmptyPerm(t *testing.T) {
	reg := registryWith(t, mw.RouteSpec{
		Method: "POST", Path: "/workbench/settings", Perm: "", Kind: mw.RoutePageWrite,
		AuthObject: "/api/project/update", AuthMethod: "POST",
	})
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"project:update": {Code: "project:update", Path: "/api/project/update", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, dir); len(problems) != 0 {
		t.Fatalf("复用对象在目录里时应通过，实际：%+v", problems)
	}
	missing := fakePermDirectory{entries: map[string]aicontract.PermEntry{}}
	problems := verifyDeclaredPermissions(context.Background(), reg, missing)
	if len(problems) != 1 || !strings.Contains(problems[0], "/api/project/update") {
		t.Fatalf("复用对象不在目录里必须报出，实际：%+v", problems)
	}
	// 豁免入口不查目录。
	exempt := registryWith(t, mw.RouteSpec{
		Method: "POST", Path: "/payment/callback", Perm: "", Kind: mw.RouteExempt,
	})
	if problems := verifyDeclaredPermissions(context.Background(), exempt, missing); len(problems) != 0 {
		t.Fatalf("明确豁免的入口不应查目录，实际：%+v", problems)
	}
}

// 禁用是合法启停：目录行存在但 status != 1 不算缺失。
func TestVerifyDeclaredPermissionsAcceptsDisabledEntries(t *testing.T) {
	reg := registryWith(t, mw.RouteSpec{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected})
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write": {Code: "a:write", Path: "/api/a", Method: "POST", Status: 0},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, dir); len(problems) != 0 {
		t.Fatalf("合法的停用不应让实例不就绪，实际：%+v", problems)
	}
}

// 目录读不出来、端口缺失、索引为空都必须 fail closed，而不是当作「无缺口」。
func TestVerifyDeclaredPermissionsFailsClosed(t *testing.T) {
	reg := registryWith(t, mw.RouteSpec{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected})
	cases := map[string][]string{
		"读取失败": verifyDeclaredPermissions(context.Background(), reg,
			fakePermDirectory{err: errors.New("db down")}),
		"端口缺失": verifyDeclaredPermissions(context.Background(), reg, nil),
		"空索引":  verifyDeclaredPermissions(context.Background(), mw.NewRegistry(), fakePermDirectory{}),
	}
	for name, problems := range cases {
		if len(problems) == 0 {
			t.Errorf("%s 必须报问题（fail closed）", name)
		}
	}
}

// AI 工具权限码必须纳入启动核对：工具不走路由，只看 Registry 会漏掉它们。
func TestVerifyDeclaredPermissionsCoversToolPermissions(t *testing.T) {
	reg := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected},
		// 工具权限码必须能进入执行授权索引（索引只收 RouteProtected），
		// 否则运行期 RouteAuthorizer 查不到绑定、工具必被拒绝。
		mw.RouteSpec{Method: "POST", Path: "/api/order/list", Perm: "order:list", Kind: mw.RouteProtected},
	)
	tools := mcp.NewRegistry()
	if err := tools.Register(mcp.New("orders_summary", "订单摘要", "查询订单", "order:list",
		mcp.Object("参数", map[string]mcp.Schema{"projectId": mcp.String("工程 id")}, "projectId"),
		func(context.Context, struct {
			ProjectID string `json:"projectId"`
		}) (mcp.Result, error) {
			return mcp.Result{}, nil
		})); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}

	// 目录缺工具权限码 → 必须报，且点名该码。
	// 它同时是受保护路由的权限点，所以路由侧与工具侧各报一条；两条都必须点名同一个码，
	// 只要有一条漏掉就说明「工具不走路由」的那次核对没接上。
	missing := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write": {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
	}}
	problems := verifyDeclaredPermissionsWithTools(context.Background(), reg, tools, missing)
	if len(problems) == 0 {
		t.Fatal("工具权限码不在目录里时必须报出")
	}
	for _, p := range problems {
		if !strings.Contains(p, "order:list") {
			t.Fatalf("每条问题都应点名 order:list，实际：%+v", problems)
		}
	}
	if !strings.Contains(strings.Join(problems, "\n"), "AI 工具权限 order:list") {
		t.Fatalf("工具侧必须单独报出（只看路由会漏掉不走路由的工具），实际：%+v", problems)
	}

	// 目录齐备（含工具权限）→ 通过。
	complete := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write":    {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"order:list": {Code: "order:list", Path: "/api/order/list", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissionsWithTools(context.Background(), reg, tools, complete); len(problems) != 0 {
		t.Fatalf("工具权限在目录里时不应报问题，实际：%+v", problems)
	}
}

// 目录行存在 ≠ 工具能被执行：执行授权索引只收 RouteProtected 绑定。
//
// 反例：工具权限码在目录里有启用行，但只登记了页面/写入口（Perm 为空或非
// RouteProtected）。运行期 RouteAuthorizer 的 index.Of(code) 为空 → 一律拒绝，
// 而启动核对若只看目录行就会报「一切正常」。
func TestVerifyDeclaredPermissionsRequiresToolBindingInIndex(t *testing.T) {
	tools := mcp.NewRegistry()
	if err := tools.Register(mcp.New("orders_summary", "订单摘要", "查询订单", "order:list",
		mcp.Object("参数", map[string]mcp.Schema{"projectId": mcp.String("工程 id")}, "projectId"),
		func(context.Context, struct {
			ProjectID string `json:"projectId"`
		}) (mcp.Result, error) {
			return mcp.Result{}, nil
		})); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"order:list": {Code: "order:list", Path: "/api/order/list", Method: "POST", Status: 1},
	}}

	// 只有页面绑定（复用别的鉴权对象）→ 必须报「没有绑定任何受保护操作」。
	pageOnly := registryWith(t, mw.RouteSpec{
		Method: "POST", Path: "/admin/orders", Perm: "", Kind: mw.RoutePageWrite,
		AuthObject: "/api/order/list", AuthMethod: "POST",
	})
	problems := verifyDeclaredPermissionsWithTools(context.Background(), pageOnly, tools, dir)
	if len(problems) != 1 || !strings.Contains(problems[0], "order:list") ||
		!strings.Contains(problems[0], "受保护操作") {
		t.Fatalf("工具码只被页面复用时必须报「索引里查不到」，实际：%+v", problems)
	}

	// 加上真实受保护绑定 → 通过。
	bound := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/admin/orders", Perm: "", Kind: mw.RoutePageWrite,
			AuthObject: "/api/order/list", AuthMethod: "POST"},
		mw.RouteSpec{Method: "POST", Path: "/api/order/list", Perm: "order:list", Kind: mw.RouteProtected},
	)
	if problems := verifyDeclaredPermissionsWithTools(context.Background(), bound, tools, dir); len(problems) != 0 {
		t.Fatalf("工具码有受保护绑定时不应报问题，实际：%+v", problems)
	}
}

// fakePolicies 是策略存在性的只读事实：目录里有行、Casbin 里可能一条策略都没有。
type fakePolicies struct {
	present map[string]bool
	err     error
	errors  map[string]error
}

func (f fakePolicies) HasPolicies(_ context.Context, code string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if err := f.errors[code]; err != nil {
		return false, err
	}
	return f.present[code], nil
}

// 目录行 ≠ 策略存在：p 策略缺失时入口对全部角色都会被拒绝，必须启动就报。
func TestVerifyDeclaredPermissionsRequiresPolicies(t *testing.T) {
	reg := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected},
		mw.RouteSpec{Method: "POST", Path: "/api/b", Perm: "b:write", Kind: mw.RouteProtected},
	)
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write": {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"b:write": {Code: "b:write", Path: "/api/b", Method: "POST", Status: 1},
	}}
	// b:write 目录有行、但一条策略都没有 → 必须点名报出。
	problems := verifyDeclaredPermissionsFull(context.Background(), reg, nil, dir,
		fakePolicies{present: map[string]bool{"a:write": true}})
	if len(problems) != 1 || !strings.Contains(problems[0], "b:write") {
		t.Fatalf("目录有行但无策略时必须点名报出，实际：%+v", problems)
	}
	// 两条都有策略 → 通过。
	if problems := verifyDeclaredPermissionsFull(context.Background(), reg, nil, dir,
		fakePolicies{present: map[string]bool{"a:write": true, "b:write": true}}); len(problems) != 0 {
		t.Fatalf("策略齐备时不应报问题，实际：%+v", problems)
	}
	// 查询失败必须 fail closed，不能当作「有策略」。
	if problems := verifyDeclaredPermissionsFull(context.Background(), reg, nil, dir,
		fakePolicies{err: errors.New("casbin down")}); len(problems) == 0 {
		t.Fatal("策略查询失败必须报问题（fail closed）")
	}
	// 合法禁用：停用行本来就不该有可用策略，不算装配损坏。
	disabled := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"a:write": {Code: "a:write", Path: "/api/a", Method: "POST", Status: 1},
		"b:write": {Code: "b:write", Path: "/api/b", Method: "POST", Status: 0},
	}}
	if problems := verifyDeclaredPermissionsFull(context.Background(), reg, nil, disabled,
		fakePolicies{present: map[string]bool{"a:write": true}}); len(problems) != 0 {
		t.Fatalf("合法停用不应让实例不就绪，实际：%+v", problems)
	}
}

// 复用和额外鉴权对象须核其策略，不得把「目录有行」冒认为「策略存在」。
func TestVerifyDeclaredPermissionsRequiresObjectPolicies(t *testing.T) {
	for name, spec := range map[string]mw.RouteSpec{
		"页面复用": {Method: "GET", Path: "/admin/seo", Kind: mw.RoutePage,
			AuthObject: "/api/seo/audit", AuthMethod: "POST"},
		"写入口复用": {Method: "POST", Path: "/admin/seo/audit", Kind: mw.RoutePageWrite,
			AuthObject: "/api/seo/audit", AuthMethod: "POST"},
		"额外检查": {Method: "POST", Path: "/api/publication/seo-audit", Perm: "publication:seo_audit",
			Kind: mw.RouteProtected, AuthObject: "/api/seo/audit", AuthMethod: "POST"},
	} {
		t.Run(name, func(t *testing.T) {
			reg := registryWith(t, spec)
			for _, tc := range []struct {
				name    string
				status  int
				present bool
				err     error
				want    string
			}{
				{name: "缺策略", status: 1, want: "没有任何策略行"},
				{name: "查询失败", status: 1, err: errors.New("object policy unavailable"), want: "读取权限策略失败"},
				{name: "合法禁用", status: 0},
				{name: "策略齐全", status: 1, present: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{
						"seo:audit":             {Code: "seo:audit", Path: "/api/seo/audit", Method: "POST", Status: tc.status},
						"publication:seo_audit": {Code: "publication:seo_audit", Path: "/api/publication/seo-audit", Method: "POST", Status: 1},
					}}
					problems := verifyDeclaredPermissionsFull(context.Background(), reg, nil, dir, fakePolicies{
						present: map[string]bool{"publication:seo_audit": true, "seo:audit": tc.present},
						errors:  map[string]error{"seo:audit": tc.err},
					})
					if tc.want == "" {
						if len(problems) != 0 {
							t.Fatalf("不应误报：%+v", problems)
						}
						return
					}
					if len(problems) != 1 || !strings.Contains(problems[0], tc.want) ||
						!strings.Contains(problems[0], "seo:audit") || !strings.Contains(problems[0], spec.Path) {
						t.Fatalf("必须定位真实入口、鉴权码及原因，实际：%+v", problems)
					}
				})
			}
		})
	}
}

// 真实装配顺序：授权索引必须在**全部路由与工具登记之后**填充，且只能填充一次。
//
// 这里用装配实例的真实快照驱动（不是手工拼 Registry）：先登记后再 Bind，
// 索引能查到后注册的绑定；冻结后迟到注册报错。
func TestAuthorizationIndexFillsAfterFullRegistration(t *testing.T) {
	reg := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/api/early", Perm: "early:write", Kind: mw.RouteProtected},
		mw.RouteSpec{Method: "POST", Path: "/api/late", Perm: "late:write", Kind: mw.RouteProtected},
	)
	index := mw.BuildPermIndex(reg.Snapshot())
	if got := index.Of("late:write"); len(got) != 1 || got[0].Path != "/api/late" {
		t.Fatalf("授权索引应包含后注册绑定，实际 %+v", got)
	}
	reg.Freeze()
	defer func() {
		if recover() == nil {
			t.Fatal("冻结后的迟到注册必须明确报错")
		}
	}()
	reg.DeclareRoute(mw.RouteSpec{Method: "POST", Path: "/api/after", Perm: "after:write", Kind: mw.RouteProtected})
}

// 目录核对失败时**不得**构造可用的权限查询：注入一个能放行的索引，
// 等于让「尚未验证的绑定」通过 AI/MCP 两个执行入口。
func TestPermIndexNotInjectedWhenVerificationFails(t *testing.T) {
	reg := registryWith(t,
		mw.RouteSpec{Method: "POST", Path: "/api/tool", Perm: "tool:run", Kind: mw.RouteProtected},
	)
	dir := fakePermDirectory{entries: map[string]aicontract.PermEntry{}} // 目录缺失 → 核对失败
	problems := verifyDeclaredPermissions(context.Background(), reg, dir)
	if len(problems) == 0 {
		t.Fatal("前提不成立：目录缺失应当报问题")
	}

	// 复现装配收尾的决策：只有核对通过才构造索引。
	var index *mw.PermIndex
	if len(problems) == 0 {
		index = mw.BuildPermIndex(reg.Snapshot())
	}
	if index != nil {
		t.Fatal("核对失败时不能构造权限查询")
	}
	if index.Has("tool:run") {
		t.Fatal("nil 索引必须对所有权限码返回未命中")
	}
	// 通过后才有可用查询。
	ok := fakePermDirectory{entries: map[string]aicontract.PermEntry{
		"tool:run": {Code: "tool:run", Path: "/api/tool", Method: "POST", Status: 1},
	}}
	if problems := verifyDeclaredPermissions(context.Background(), reg, ok); len(problems) != 0 {
		t.Fatalf("目录齐备时不应报问题：%+v", problems)
	}
	index = mw.BuildPermIndex(reg.Snapshot())
	if !index.Has("tool:run") {
		t.Fatal("核对通过后索引应能查到受保护操作")
	}
}

// 双执行入口（站内 Runner 与外部 MCP）共用同一份不可变查询：
// 没有绑定的权限码在两个入口都必须判拒绝，不能一处放行一处拒绝。
func TestBothExecutionEntriesRejectUnboundPerm(t *testing.T) {
	index := mw.BuildPermIndex([]mw.RouteSpec{
		{Method: "POST", Path: "/api/bound", Perm: "bound:run", Kind: mw.RouteProtected},
	})
	for _, perm := range []string{"unbound:run", ""} {
		if index.Has(perm) {
			t.Errorf("权限码 %q 未绑定，两个执行入口都必须判拒绝", perm)
		}
	}
	if !index.Has("bound:run") {
		t.Fatal("已绑定权限码应命中")
	}
}
