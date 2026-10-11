package templates

// admin_pagination_probe_p0_test.go — 分页迁移的 P0b「Jet 真探针」（task-97；设计见 task-94 v2）。
//
// P0a 冻结的是**旧数据形态**渲染出来的字节。P0b 回答另一个问题：迁移的目标形态
// （`data["Pagination"]` 注入一个**值**、partial 用 `isset` + 方法调用 + `range` 自己构造）
// **这套 Jet 惯用法在本仓的 Jet 版本下真的渲染得出来吗**，缺键/单页/空集时会不会炸。
//
// === 探针跑出来的两条硬结论（task-97 当场发现，直接改写 P1/P2 的模板写法）===
//
// 1. **裸方法名不是调用。** `{{if pg.HasPagination}}`（无括号）在本仓 Jet v6.3.2 下**不会调用方法**，
//    它取到的是「方法值」，渲染出来是函数地址（`0x51fae0`），放进 `{{if}}` 里**恒为真**——
//    于是单页列表也会进分页分支，随后访问分页数据键才炸出运行时错误。
//    正确写法是**带括号**：`{{if pg.HasPagination()}}` → 真/假（值与指针对象都成立）。
//    全仓模板此前**零处**带括号的方法调用（`rg '\{\{[^}]*\.[A-Z][A-Za-z0-9_]*\(\)'` 无匹配），
//    现有 `{{if .HasAPIKey}}` 之类全是**结构体字段**（Go 侧算好的 bool），不是方法 ——
//    也就是说这套「模板里调方法」是本迁移**首次引入**的写法，必须由探针与 P1 测试钉住。
//    `TestP0ProbePaginationBareMethodIsNotACall` 是常驻绊线：哪天升级 Jet 让裸名变成调用，
//    它会红，提醒重新评估（而不是悄悄改变页面行为）。
//
// 2. **`isset` 会把「类型化 nil 指针」当成未设置 —— 这正是守卫必须存在的原因。**
//    实测 `var pg *p0PageMirror; data["Pagination"] = pg` 时 `{{isset(.Pagination)}}` 为 **false**，
//    于是 `{{if isset(...)}}` 守卫下整块分页条不渲染：**误注入 nil 也不会 500**（静默降级，可接受）。
//    但**去掉守卫**就完全不同：`{{pg := .pg}}{{if pg.HasPagination()}}` 直接进程级 panic
//    （`value method … called using nil *p0PageMirror pointer`，Jet 捕获后又 panic 出去）。
//    结论进 P1/P2：①`Page` 用**值**类型；②partial 的第一层必须是 `isset(.Pagination)`，
//    它同时兜住「键不存在」与「被误注入 nil 指针」两种装配缺陷。
//
// 本文件不写生产代码（task-97 写域内只新增测试）。结构体 `p0PageMirror` 是 task-94 v2 定稿
// `pagination.Page` 的**替身**，不是它本身：探针证明「这套惯用法可渲染 + 降级安全」，
// 「真实类型确实具备这些字段/方法」由 P1 自己的单元测试绑定（见文件末尾交接说明）。
//
// 探针模板全部走内存 Set（`memSet`），不碰磁盘上的真实 partial。

import (
	"strings"
	"testing"
)

// p0PageMirror 镜像 task-94 v2 定稿的 `pagination.Page`（值语义 + 值接收者方法）。
// P1 落地真实类型后，本替身应删除，或改为「真实类型必须满足同一组惯用法」的对照。
type p0PageMirror struct {
	Total     int64
	Page      int
	Limit     int
	Pages     int
	Start     int
	End       int
	PageParam string
	KeepLimit bool
	Base      string
}

func (p p0PageMirror) HasPagination() bool { return p.Pages > 1 }
func (p p0PageMirror) HasPrev() bool       { return p.Page > 1 }
func (p p0PageMirror) HasNext() bool       { return p.Page < p.Pages }
func (p p0PageMirror) PrevPage() int       { return p.Page - 1 }
func (p p0PageMirror) NextPage() int       { return p.Page + 1 }

// p0LinkMirror 镜像目标形态里「链接事实」（值 + 四个字段），用于 range 探针。
type p0LinkMirror struct {
	Label    string
	URL      string
	Active   bool
	Disabled bool
}

// p0ProbeTemplate 目标 partial 的等价骨架：isset 守卫 → 方法调用（**必须带括号**）→ 字段插值 → range。
const p0ProbeTemplate = `
{{if isset(.Pagination)}}
{{pg := .Pagination}}
{{if pg.HasPagination()}}
<nav class="pagination">
    <div class="pagination-info">共 {{pg.Total}} 条，第 {{pg.Start}}-{{pg.End}} 条</div>
    <div class="pagination-pages">
        {{if pg.HasPrev()}}<a class="pagination-btn" href="p{{pg.PrevPage()}}">上一页</a>{{else}}<span class="pagination-btn is-disabled">上一页</span>{{end}}
        {{range _, n := .ProbePages}}<span class="pagination-btn">{{n}}</span>{{end}}
        {{range _, l := .ProbeLinks}}<a class="pagination-btn" href="{{l.URL}}">{{l.Label}}</a>{{end}}
        {{if pg.HasNext()}}<a class="pagination-btn" href="p{{pg.NextPage()}}">下一页</a>{{else}}<span class="pagination-btn is-disabled">下一页</span>{{end}}
    </div>
</nav>
{{end}}
{{end}}
`

// p0ProbeRender 用内存 Set 渲染给定模板；返回输出与错误（错误必须由调用方断言）。
// panic 不在此拦截：调用方按用例需要自行 recover（类型化 nil 那档就是要看见 panic）。
func p0ProbeRender(t *testing.T, tpl string, data map[string]any) (string, error) {
	t.Helper()
	set := memSet(t, map[string]string{"probe/pagination.html": tpl})
	tmpl, err := set.GetTemplate("probe/pagination.html")
	if err != nil {
		t.Fatalf("编译探针模板失败: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, nil, data); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// TestP0ProbePaginationIdiomsDegradeSafely 目标惯用法在「没东西可显示」的三档上必须静默降级：
// 不报错、不出现 `.pagination`。这是 P2 能安全上线的下限。
//
// 后三档的 data 里**刻意不放** ProbePages/ProbeLinks：分页分支一旦被误进（例如有人把括号丢了），
// 就会以运行时错误暴露，而不是渲染出半截分页条。
func TestP0ProbePaginationIdiomsDegradeSafely(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{name: "键缺失", data: map[string]any{}},
		{name: "零值事实", data: map[string]any{"Pagination": p0PageMirror{}}},
		{name: "单页", data: map[string]any{"Pagination": p0PageMirror{Total: 5, Page: 1, Limit: 20, Pages: 1, Start: 1, End: 5}}},
		{name: "空集", data: map[string]any{"Pagination": p0PageMirror{Total: 0, Page: 1, Limit: 20, Pages: 0, Start: 0, End: 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := p0ProbeRender(t, p0ProbeTemplate, tc.data)
			if err != nil {
				t.Fatalf("该档必须静默降级，实际报错: %v", err)
			}
			if strings.Contains(out, "pagination") {
				t.Fatalf("该档不该渲染分页条，实际输出: %q", out)
			}
		})
	}
}

// TestP0ProbePaginationIdiomsRenderFacts 有分页时，探针必须把事实渲染成可验证的形态：
// int64/int 插值、值接收者方法、`[]int` 与 `[]struct` 两种 range。
func TestP0ProbePaginationIdiomsRenderFacts(t *testing.T) {
	data := map[string]any{
		"Pagination": p0PageMirror{Total: 120, Page: 2, Limit: 20, Pages: 6, Start: 21, End: 40},
		"ProbePages": []int{1, 2, 3, 6},
		"ProbeLinks": []p0LinkMirror{{Label: "首页", URL: "/admin/mail?account_page=1"}},
	}
	out, err := p0ProbeRender(t, p0ProbeTemplate, data)
	if err != nil {
		t.Fatalf("探针渲染失败: %v", err)
	}
	for _, want := range []string{
		`<nav class="pagination">`,
		"共 120 条，第 21-40 条",                       // int64 插值
		`href="p1"`,                               // PrevPage() 方法调用
		`href="p3"`,                               // NextPage()
		"<span class=\"pagination-btn\">1</span>", // []int range 里的 int 插值
		`href="/admin/mail?account_page=1"`,       // []struct range 里的 string 字段
		"首页",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q\n实际: %q", want, out)
		}
	}
	if strings.Contains(out, "is-disabled") {
		t.Fatalf("第 2/6 页不该出现禁用按钮，实际: %q", out)
	}
	if strings.Contains(out, "%!") || strings.Contains(out, "%s") {
		t.Fatalf("插值留下了未消费的格式符，实际: %q", out)
	}
}

// TestP0ProbePaginationValueAndPointerRenderSameIdioms 值语义与指针语义在这套惯用法下必须同形：
// P1 一律注入值，但只要有任何调用点传了指针，渲染也应完全一致（否则是「同一份模板、两种页面」）。
func TestP0ProbePaginationValueAndPointerRenderSameIdioms(t *testing.T) {
	pg := p0PageMirror{Total: 120, Page: 6, Limit: 20, Pages: 6, Start: 101, End: 120}
	extras := map[string]any{"ProbePages": []int{1, 6}, "ProbeLinks": []p0LinkMirror{{Label: "首页", URL: "/admin/mail?account_page=1"}}}
	byValue, err := p0ProbeRender(t, p0ProbeTemplate, p0WithExtras(map[string]any{"Pagination": pg}, extras))
	if err != nil {
		t.Fatalf("值语义渲染失败: %v", err)
	}
	byPointer, err := p0ProbeRender(t, p0ProbeTemplate, p0WithExtras(map[string]any{"Pagination": &pg}, extras))
	if err != nil {
		t.Fatalf("指针语义渲染失败: %v", err)
	}
	if byValue != byPointer {
		t.Fatalf("值与指针渲染不一致:\n值: %q\n指针: %q", byValue, byPointer)
	}
	if !strings.Contains(byValue, "is-disabled") {
		t.Fatalf("末页必须禁用下一页，实际: %q", byValue)
	}
}

// TestP0ProbePaginationBareMethodIsNotACall 常驻绊线：裸方法名（无括号）在本仓 Jet 版本下**不是调用**。
//
// 断言的是「它渲染成函数地址、且在 `{{if}}` 里恒为真」这一现实 —— 这正是 P1/P2 必须写括号的原因。
// 若某天 Jet 升级让裸名变成真调用，本用例会红：那时要重新评估的是**已经写下的模板**（可能有
// 人照着旧结论写了裸名），而不是把这条断言改掉。
func TestP0ProbePaginationBareMethodIsNotACall(t *testing.T) {
	const tpl = `{{pg := .Pagination}}bare=[{{pg.HasPagination}}] {{if pg.HasPagination}}entered{{end}} paren=[{{pg.HasPagination()}}]`
	out, err := p0ProbeRender(t, tpl, map[string]any{"Pagination": p0PageMirror{Total: 5, Page: 1, Limit: 20, Pages: 1, Start: 1, End: 5}})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "entered") {
		t.Fatalf("单页却未进入裸名分支 —— 说明裸名已经变成真调用，需要重写本结论与所有模板写法: %q", out)
	}
	if !strings.Contains(out, "bare=[0x") {
		t.Fatalf("裸名应当渲染成函数地址（非 true/false），实际: %q", out)
	}
	if !strings.Contains(out, "paren=[false]") {
		t.Fatalf("带括号必须给出真布尔值 false，实际: %q", out)
	}
}

// TestP0ProbePaginationTypedNilUnderGuardDegradesSafely 类型化 nil 在 `isset` 守卫下**安全降级**：
// Jet 的 `isset` 把 nil 指针当「未设置」，于是分页条整块不渲染 —— 装配缺陷不会变成 500。
// 这是 partial 第一层必须写 `{{if isset(.Pagination)}}` 的直接依据。
func TestP0ProbePaginationTypedNilUnderGuardDegradesSafely(t *testing.T) {
	var pg *p0PageMirror
	out, err := p0ProbeRender(t, p0ProbeTemplate, map[string]any{"Pagination": pg})
	if err != nil {
		t.Fatalf("带 isset 守卫时类型化 nil 必须静默降级，实际报错: %v", err)
	}
	if strings.Contains(out, "pagination") {
		t.Fatalf("带 isset 守卫时类型化 nil 不得渲染分页条，实际: %q", out)
	}
	// 同一份探针数据的 isset 判定本身也钉住：守卫依赖的正是「nil 指针 === 未设置」。
	guarded, err := p0ProbeRender(t, `{{if isset(.Pagination)}}set{{else}}unset{{end}}`, map[string]any{"Pagination": pg})
	if err != nil {
		t.Fatalf("isset 探针渲染失败: %v", err)
	}
	if guarded != "unset" {
		t.Fatalf("isset 必须把类型化 nil 视为未设置，实际: %q", guarded)
	}
}

// TestP0ProbePaginationUnguardedTypedNilPanics 去掉守卫就完全不同：直接进程级 panic。
// 这条不是「要支持无守卫写法」，而是把「守卫不是可选项」变成可执行事实 ——
// 谁在 P2 里把 `isset` 删掉，上面那条与这条会一起说明后果。
func TestP0ProbePaginationUnguardedTypedNilPanics(t *testing.T) {
	var pg *p0PageMirror
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = p0ProbeRender(t, `{{pg := .Pagination}}{{if pg.HasPagination()}}nav{{end}}`, map[string]any{"Pagination": pg})
	}()
	if recovered == nil {
		t.Fatalf("无 isset 守卫时访问类型化 nil 必须 panic（本约定依赖此事实）")
	}
	t.Logf("无守卫时的实际表现: %v", recovered)
}

// p0WithExtras 合并分页事实与探针用的额外键（range 数据），避免每个用例重复三行。
func p0WithExtras(base, extras map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extras))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extras {
		out[k] = v
	}
	return out
}
