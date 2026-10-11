package templates

// admin_pagination_dual_shape_p2_test.go — 分页条「双形态」测试（task-102 = task-94 的 P2）。
//
// 这一卡只做一件事：`admin/partials/pagination.html` 同时支持
//   · 新：`data["Pagination"]` = `pkg/pagination.Page`（事实层；partial 自己算信息行/按钮/链接/选中态）
//   · 旧：`data["PaginationInfo"]` / `data["PaginationLinks"]`（Go 预制成品，迁移期兜底）
// 并保证**新形态渲染出的字节与 P0a 冻结 golden 逐字节相同** —— 也就是说 P2 之后页面输出零变化。
//
// 为什么旧路径也要在这里再测一遍：Sol 在 P2 设计审阅里要求「旧正文逐字保留」不能只靠静态字节论证
// （那只是我推的），必须**实测**。下面 TestP2LegacyPathUnchanged 就是那次实测：把新文件按旧键渲染，
// 与冻结 golden 比对；任何"顺手美化"了旧正文的改动都会在这里红。
//
// 形态 7（product 展开区 rows=0 的「第 0-0 条」）**不参与新路径断言**：它是**合成的 partial 夹具**
// （golden 里手写了 info/links），真实的 product 片段（`admin/product/product_tag_hits.html:44-48`）
// 自持 nav/PageInfo/PrevURL/NextURL，根本不消费本 partial。它的迁移属 P3 的 B2 批次。
//
// 另一条覆盖纠正（由 Sol 指出、我程序化复核确认）：P0a-2（total=1000/page=50/limit=20）与
// P0a-4（末页）的冻结字节**完全相同** —— 因为 page=50 就是第 50 页，所以 P0 里其实**没有**
// 中段双侧省略号形态。本文件用 TestP2MiddlePageHasTwoEllipses 补上（page=25 / 50 页），
// 并且**不改冻结 golden**。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/pkg/pagination"
)

const p2PartialPath = "admin/partials/pagination.html"

// p2Render 渲染 partial（真实 Jet + 磁盘模板；t 由传入 data 决定）。
func p2Render(t *testing.T, data map[string]any) string {
	t.Helper()
	out, err := render(t, newAdminTestSet(), p2PartialPath, data)
	if err != nil {
		t.Fatalf("渲染分页条失败: %v", err)
	}
	return out
}

// p2RenderLegacy 按**旧键**渲染（迁移期真实形态：31 个消费者注入的就是这两个键）。
func p2RenderLegacy(t *testing.T, s p0PaginationShape) string {
	t.Helper()
	data := adminShellData()
	for k, v := range p0PaginationKeys(t, s) {
		data[k] = v
	}
	return p2Render(t, data)
}

// p2RenderFacts 按**新事实**渲染。override 用于 mail 那种「参数改名 + 不带 limit」的调用点。
func p2RenderFacts(t *testing.T, s p0PaginationShape, override func(*pagination.Page)) string {
	t.Helper()
	data := p2NewFactsData(t, s, override)
	return p2Render(t, data)
}

func p2NewFactsData(t *testing.T, s p0PaginationShape, override func(*pagination.Page)) map[string]any {
	t.Helper()
	pg := pagination.Facts(s.total, s.page, s.limit)
	pg.Base = s.base
	if override != nil {
		override(&pg)
	}
	data := adminShellData()
	data["Pagination"] = pg
	return data
}

// p2FirstDiff 报告首个差异偏移与上下文。
//
// 为什么不用整串 %q：P2 的主要风险是**白空间槽位**（不是逻辑），整串对比会打印上千字符，
// 而真正要调的信息只有"第几个字节开始不一样、两边各自长什么样"。这条诊断把调准变成机械动作。
func p2FirstDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	i := 0
	for i < n && want[i] == got[i] {
		i++
	}
	window := func(s string) string {
		lo := i - 30
		if lo < 0 {
			lo = 0
		}
		hi := i + 40
		if hi > len(s) {
			hi = len(s)
		}
		return fmt.Sprintf("%q", s[lo:hi])
	}
	return fmt.Sprintf(
		"首个差异在偏移 %d（期望长 %d / 实际长 %d）\n  期望[%d:]: %s\n  实际[%d:]: %s",
		i, len(want), len(got), i, window(want), i, window(got))
}

// TestP2LegacyPathUnchanged 旧路径逐字不变（Sol 要求："实测，不只静态论证"）。
func TestP2LegacyPathUnchanged(t *testing.T) {
	for _, shape := range p0PaginationShapes() {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			got := p2RenderLegacy(t, shape)
			want := p0PaginationGolden[shape.name]
			if got != want {
				t.Fatalf("旧键路径字节变了（说明旧正文被改动）：\n%s", p2FirstDiff(want, got))
			}
		})
	}
}

// TestP2FactsPathMatchesFrozenGolden 新事实路径与同一份冻结 golden 逐字节相同。
func TestP2FactsPathMatchesFrozenGolden(t *testing.T) {
	for _, shape := range p0PaginationShapes() {
		shape := shape
		if shape.name == "P0a-7-product片段rows为零" {
			continue // 合成夹具，不走 partial 新路径（见文件头说明）
		}
		t.Run(shape.name, func(t *testing.T) {
			var override func(*pagination.Page)
			if shape.name == "P0a-6-mail参数改名无limit" {
				// mail_page.go 的形态：页码参数改名、链接不带 limit。
				override = func(pg *pagination.Page) {
					pg.PageParam = "account_page"
					pg.KeepLimit = false
				}
			}
			got := p2RenderFacts(t, shape, override)
			want := p0PaginationGolden[shape.name]
			if got != want {
				t.Fatalf("新事实路径字节与冻结 golden 不一致：\n%s", p2FirstDiff(want, got))
			}
		})
	}
}

// p2ShapeByName 从 P0 形态表里取一个形态（避免在本文件里复制数字，数字抄错就变成"测自己"）。
func p2ShapeByName(t *testing.T, name string) p0PaginationShape {
	t.Helper()
	for _, s := range p0PaginationShapes() {
		if s.name == name {
			return s
		}
	}
	t.Fatalf("P0 形态表里没有 %q —— 表被改过？", name)
	return p0PaginationShape{}
}

// TestP2MiddlePageHasTwoEllipses 补 P0 的覆盖缺口：中段页必须出现**两个**省略号，且新旧路径逐字相同。
//
// 为什么必须补：P0a-2 名字叫"多页双省略号"，但它的 page=50 就是第 50 页（末页），
// 冻结字节与 P0a-4 完全相同（下面第一条断言把这个事实钉住）—— 于是"中段双侧 gap"在 P0 里
// 根本没被覆盖。这里用 page=25 / 50 页补上，并顺带比首/末贴边。
func TestP2MiddlePageHasTwoEllipses(t *testing.T) {
	// 先钉住 Sol 指出的事实：P0a-2 就是末页，与 P0a-4 字节相同。
	shape2 := p2ShapeByName(t, "P0a-2-多页双省略号")
	if pages := pagination.Facts(shape2.total, shape2.page, shape2.limit).Pages; shape2.page != pages {
		t.Fatalf("P0a-2 的 page=%d 不再是末页(%d)，本文件的覆盖说明需同步更新", shape2.page, pages)
	}
	if p0PaginationGolden["P0a-2-多页双省略号"] != p0PaginationGolden["P0a-4-末页next禁用"] {
		t.Fatalf("P0a-2 与 P0a-4 不再逐字节相同 —— 冻结 golden 被改过？")
	}

	mid := p0PaginationShape{name: "P2-中段双侧省略号", total: 1000, page: 25, limit: 20, base: "/admin/mail"}
	gotOld := p2RenderLegacy(t, mid)
	gotNew := p2RenderFacts(t, mid, nil)
	if gotNew != gotOld {
		t.Fatalf("中段页新旧路径不一致：\n%s", p2FirstDiff(gotOld, gotNew))
	}
	if n := strings.Count(gotNew, "…"); n != 2 {
		t.Fatalf("中段页应有两个省略号，实际 %d 个：%q", n, gotNew)
	}
	if n := strings.Count(gotNew, `class="pagination-btn is-disabled">…<`); n != 2 {
		t.Fatalf("两个省略号都应是禁用态 span，实际 %d 个", n)
	}
	// 首页标题与末页标题在两侧贴边（单侧省略号），新旧路径也要逐字相同。
	for _, page := range []int{1, 50} {
		edge := p0PaginationShape{name: "P2-贴边", total: 1000, page: page, limit: 20, base: "/admin/mail"}
		if gotNew, gotOld := p2RenderFacts(t, edge, nil), p2RenderLegacy(t, edge); gotNew != gotOld {
			t.Fatalf("page=%d 贴边档新旧路径不一致：\n%s", page, p2FirstDiff(gotOld, gotNew))
		}
	}
}

// TestP2FactsWinWhenBothProvided 两种数据同时存在时**新形态优先**（含 Sol 点名的两个边界）。
func TestP2FactsWinWhenBothProvided(t *testing.T) {
	legacy := p0PaginationKeys(t, p2ShapeByName(t, "P0a-2-多页双省略号"))

	t.Run("新事实有效+旧键也有效", func(t *testing.T) {
		mid := p0PaginationShape{name: "P2-优先", total: 1000, page: 25, limit: 20, base: "/admin/mail"}
		data := p2NewFactsData(t, mid, nil)
		for k, v := range legacy {
			data[k] = v
		}
		got := p2Render(t, data)
		want := p2RenderFacts(t, mid, nil)
		if got != want {
			t.Fatalf("同时给出时应走新形态：\n%s", p2FirstDiff(want, got))
		}
	})

	t.Run("新零值+旧键有效", func(t *testing.T) {
		data := adminShellData()
		data["Pagination"] = pagination.Page{}
		for k, v := range legacy {
			data[k] = v
		}
		if got := p2Render(t, data); strings.Contains(got, "pagination") {
			t.Fatalf("新形态是零值时必须不渲染（不得回落到旧键），实际: %q", got)
		}
	})

	t.Run("新单页+旧键有效", func(t *testing.T) {
		data := adminShellData()
		data["Pagination"] = pagination.Facts(5, 1, 20)
		for k, v := range legacy {
			data[k] = v
		}
		if got := p2Render(t, data); strings.Contains(got, "pagination") {
			t.Fatalf("新形态是单页时必须不渲染（不得回落到旧键），实际: %q", got)
		}
	})
}

// TestP2DegradesWithoutData 没有可渲染数据时静默降级：不报错、不渲染 `.pagination`。
//
// typed nil 一档按**现有 isset 语义**验证（lead 明确：不为探针改 Facts 的值类型）：
// 误注入 `(*pagination.Page)(nil)` 时 `isset(.Pagination)` 为假 → 按"没注入"处理，
// 于是有旧键就回落旧键、没有就什么都不渲染。这是 P0b 实测结论在真实 partial 上的复核。
func TestP2DegradesWithoutData(t *testing.T) {
	var nilPage *pagination.Page

	cases := map[string]map[string]any{
		"缺键":        adminShellData(),
		"零值":        {"Pagination": pagination.Page{}},
		"单页":        {"Pagination": pagination.Facts(5, 1, 20)},
		"恰好一页":      {"Pagination": pagination.Facts(20, 1, 20)},
		"空集":        {"Pagination": pagination.Facts(0, 1, 20)},
		"越界页码":      {"Pagination": pagination.Facts(0, 3, 20)},
		"typed nil": {"Pagination": nilPage},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			data := adminShellData()
			for k, v := range extra {
				data[k] = v
			}
			got := p2Render(t, data)
			if strings.Contains(got, "pagination") {
				t.Fatalf("该档不该渲染分页条，实际: %q", got)
			}
		})
	}

	t.Run("typed nil + 旧键有效 → 按未注入处理，回落到旧键", func(t *testing.T) {
		shape := p2ShapeByName(t, "P0a-3-首页prev禁用")
		data := adminShellData()
		data["Pagination"] = nilPage
		for k, v := range p0PaginationKeys(t, shape) {
			data[k] = v
		}
		got := p2Render(t, data)
		if want := p0PaginationGolden[shape.name]; got != want {
			t.Fatalf("typed nil 应回落到旧键：\n%s", p2FirstDiff(want, got))
		}
	})
}

// TestP2TranslationChainIsIntact 取词链：四个词条都经 t()，信息行的数字经 sprintf 位置填充。
//
// 用替身 t（`«原文»`）而不是真库：本包测试进程不初始化 i18n，真库路径取不到词，
// 那样测出来的是"回退"，证明不了"四个键都被查过、数字被填进去"。
func TestP2TranslationChainIsIntact(t *testing.T) {
	mid := p0PaginationShape{name: "P2-取词链", total: 1000, page: 25, limit: 20, base: "/admin/mail"}
	data := p2NewFactsData(t, mid, nil)
	data["t"] = func(_, fallback string) string { return "«" + fallback + "»" }
	got := p2Render(t, data)

	for _, want := range []string{
		"aria-label=\"«分页»\"",
		"«共 1000 条，第 481-500 条»", // 词条原文 + sprintf 填进去的两个数字
		"«上一页»",
		"«下一页»",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("取词链缺少 %q，实际: %q", want, got)
		}
	}
}

// TestP2LanguageFallbackDoesNotLeakKeys zh-CN / en-US 两条语言链都必须能渲染出完整分页条，
// 且**不得漏出裸 key**（词条缺失时用的是模板里写的中文原文兜底 —— 这正是缺词口径）。
func TestP2LanguageFallbackDoesNotLeakKeys(t *testing.T) {
	shape := p0PaginationShape{name: "P2-语言", total: 45, page: 1, limit: 20, base: "/admin/mail"}
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			data := p2NewFactsData(t, shape, nil)
			data["t"] = TranslateFunc(lang)
			got := p2Render(t, data)
			if strings.Contains(got, "shell.pagination.") {
				t.Fatalf("不得渲染出裸 key，实际: %q", got)
			}
			if !strings.Contains(got, `class="pagination"`) {
				t.Fatalf("该形态应渲染出分页条，实际: %q", got)
			}
			// 缺词路径：词条取不到时必须落到模板里的兜底文案（这里是中文原文），
			// 而不是渲染出空串或裸 key。i18n 未初始化正是这条路径的真实形态。
			for _, want := range []string{"共 45 条，第 1-20 条", "上一页", "下一页"} {
				if !strings.Contains(got, want) {
					t.Fatalf("%s：缺词应回落到模板兜底文案 %q，实际: %q", lang, want, got)
				}
			}
		})
	}
}

// —— 以下是"旧分支不能悄悄常驻"的护栏 ——

// p2PartialSource 读 partial 源码（P4 删除旧分支前，这些护栏一直有效）。
func p2PartialSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("admin", "partials", "pagination.html"))
	if err != nil {
		t.Fatalf("读取 partial 失败: %v", err)
	}
	return string(src)
}

// p2CodeOnly 去掉 Jet 注释（`{* … *}`）——护栏只看会执行的模板文本，不看说明文字。
func p2CodeOnly(src string) string {
	for {
		i := strings.Index(src, "{*")
		if i < 0 {
			return src
		}
		j := strings.Index(src[i:], "*}")
		if j < 0 {
			return src[:i]
		}
		src = src[:i] + src[i+j+2:]
	}
}

// TestP2KeepsSingleLegacyFallback 回退口**恰好一处**：防止迁移期再叠第二/第三种兜底形态。
func TestP2KeepsSingleLegacyFallback(t *testing.T) {
	code := p2CodeOnly(p2PartialSource(t))
	// 回退**口**只有一个（{{else if …}}）；"PaginationLinks" 出现 2 次是正常的：
	// 回退条件 1 次 + 旧正文里读取该键 1 次。数键名会误判，必须数回退条件本身。
	if n := strings.Count(code, `{{else if .["PaginationLinks"]}}`); n != 1 {
		t.Fatalf("回退条件应恰好 1 处，实际 %d 处", n)
	}
	if n := strings.Count(code, `"PaginationLinks"`); n != 2 {
		t.Fatalf("旧键引用应恰好 2 处（回退条件 + 旧正文读取），实际 %d 处", n)
	}
	if n := strings.Count(code, `"PaginationInfo"`); n != 1 {
		t.Fatalf("旧信息行键应恰好 1 处，实际 %d 处", n)
	}
}

// TestP2MethodCallsAreParenthesized 方法调用必须带括号（P0b 实测：裸名是函数值、恒为真）。
func TestP2MethodCallsAreParenthesized(t *testing.T) {
	code := p2CodeOnly(p2PartialSource(t))
	for _, name := range []string{"HasPagination", "HasPrev", "HasNext"} {
		for idx := 0; ; {
			i := strings.Index(code[idx:], name)
			if i < 0 {
				break
			}
			at := idx + i
			rest := code[at+len(name):]
			if !strings.HasPrefix(rest, "(") {
				t.Fatalf("%s 出现处没有紧跟 `(` —— 裸方法名在 Jet 下恒为真，会把单页也渲染成分页条", name)
			}
			idx = at + len(name)
		}
	}
}

// TestP2KeepsP4RemovalMarker P4 删除标记必须在，且必须是 Jet 注释（不发射到 HTML）。
func TestP2KeepsP4RemovalMarker(t *testing.T) {
	src := p2PartialSource(t)
	const marker = "REFACTOR(step P4): 删除旧 Info/Links 分支"
	if !strings.Contains(src, marker) {
		t.Fatalf("缺少 P4 删除标记 %q —— 迁移窗口会失去收口提示", marker)
	}
	if !strings.Contains(src, "{*") {
		t.Fatalf("标记必须在 Jet 注释里（否则会被发射到 HTML）")
	}
	shape := p2ShapeByName(t, "P0a-1-单页内无省略号")
	if got := p2RenderLegacy(t, shape); strings.Contains(got, "REFACTOR") {
		t.Fatalf("注释发射进了 HTML: %q", got)
	}
}

// TestP2LegacyConsumerCountOnlyShrinks 生产 `BuildPagination(` 可执行调用点**只减不增**（基线 31）。
//
// 这是"旧分支不回归"的真正防线：P3 每批把消费者切到事实层，这个计数必须跟着降；
// 谁在迁移期新建了一个注入旧键的页面（或忘了切而复制粘贴），这条会红。
// 口径：非 `_test.go`、剔除整行 `//` 注释后的 `.BuildPagination(` 出现次数
// （带点号即排除 `pkg/pagination` 里的函数定义那一行）。
func TestP2LegacyConsumerCountOnlyShrinks(t *testing.T) {
	const baseline = 31
	var files []string
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name := info.Name(); name == ".git" || name == "tmp" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库失败: %v", err)
	}
	count := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			count += strings.Count(line, ".BuildPagination(")
		}
	}
	if count > baseline {
		t.Fatalf("生产 BuildPagination 调用点 %d 个，超过基线 %d —— 迁移期不得新增旧形态调用点", count, baseline)
	}
	t.Logf("生产 BuildPagination 调用点 = %d（基线 %d，P3 只减）", count, baseline)
}

// TestP2LiteralSlotEmissionIsVerbatim 钉住 partial 里那条**显式白空间字面量**的行为假设。
//
// partial 的末条目用 `{{"\n        \n        "}}` 精确发两个槽（理由写在模板注释里：行边界会被
// {{if}}/{{end}} 吞掉）。这条依赖 Jet 对插值字面量「只做 HTML 转义、不动控制字符」：
// 若哪天它把换行/制表符也转义（或吞掉），页面白空间会静默变样 —— 这里就会红。
func TestP2LiteralSlotEmissionIsVerbatim(t *testing.T) {
	out, err := p0plusRenderData(t, `A{{"\n        \n\t&<"}}B`, nil)
	if err != nil {
		t.Fatalf("渲染探针失败: %v", err)
	}
	want := "A\n        \n\t&amp;&lt;B"
	if out != want {
		t.Fatalf("字面量发射被改变：want %q, got %q", want, out)
	}
}
