package feature

// product_list_filter_paging_test.go — 商品域后台子列表（属性 / 分类 / 品牌 / 标签）的
// 筛选入口与分页链路（审计 02-M 的 D12 / D13）。
//
// 三条断言都从**渲染出来的 HTML** 上取，而不是「我们写下的配置」：
//
//	① 筛选栏存在，且关键词回显在控件上（提交后看不出筛了什么 = 筛选栏白加）；
//	② 数据超过一页时分页条出现，且翻页链接**保留筛选条件**（条件丢了用户会以为筛选失效）；
//	③ 筛选无结果时空态仍保留 <table>/<thead>，并说清「是筛出来的空」而不是「还没有数据」。
//
// 用真实 Jet 模板 + 真实 service（与生产同一 template root、同一套迁移建的表结构）。
// 四个页面各自复用同包已有的测试引擎：newTaxonomyPageEngine（分类 / 品牌）、
// newTagPageEngine（标签）、newAttrPageEngine（属性）。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/public/migrations"
)

// getWholePage 取页面 HTML，并额外要求渲染完整（出现 </html>）。
//
// 为什么不直接用同包的 getPage：缺数据键时 Jet 在那一行中断渲染，**HTTP 仍是 200**，
// 断言只看状态码就会把「后半页整块消失」当成通过。
func getWholePage(t *testing.T, engine *gin.Engine, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "</html>") {
		t.Fatalf("页面渲染不完整：GET %s → %d，body=%s", target, rec.Code, clip(body, 600))
	}
	return body
}

// clip 只用于失败信息，避免把整页 HTML 打进日志。
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// paginationSection 摘出分页条的 HTML 段（判「翻页链接里有没有保留筛选条件」用）。
//
// 不能在整个页面里搜 keyword=xxx：筛选栏的回显 value 与批量表单的隐藏域也含同样的字面量，
// 那样即使分页链接丢了条件，断言照样通过。
func paginationSection(body string) string {
	i := strings.Index(body, `class="pagination"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest, "</nav>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestProductListFilterBarKeywordEcho ① 四页都有筛选栏，关键词回显在输入框上。
func TestProductListFilterBarKeywordEcho(t *testing.T) {
	taxonomy, _ := newTaxonomyPageEngine(t)
	if taxonomy == nil {
		return
	}
	tags, _ := newTagPageEngine(t)
	if tags == nil {
		return
	}
	attrs, _ := newAttrPageEngine(t)
	if attrs == nil {
		return
	}

	pages := []struct {
		name        string
		engine      *gin.Engine
		path        string
		id          string
		placeholder string
	}{
		{"分类", taxonomy, "/admin/product-categories?keyword=zzz", "categories-filter-keyword", "分类名 / URL 段"},
		{"品牌", taxonomy, "/admin/product-brands?keyword=zzz", "brands-filter-keyword", "品牌名 / URL 段"},
		{"标签", tags, "/admin/product-tags?keyword=zzz", "tags-filter-keyword", "标签名 / URL 段"},
		{"属性", attrs, "/admin/product-attributes?keyword=zzz", "attributes-filter-keyword", "名称 / 标识"},
	}
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			body := getWholePage(t, p.engine, p.path)
			for _, want := range []string{
				`class="filter-bar"`,
				`class="filter-fields"`,
				`name="keyword"`,
				`id="` + p.id + `"`,
				// 回显：提交后条件必须还留在控件上（这也是「分页链接会不会丢条件」的前置）。
				`value="zzz"`,
				`placeholder="` + p.placeholder + `"`,
				`class="filter-actions"`,
				// 筛选栏是 GET 表单：服务端渲染、无 JS 也能用，条件落在查询串上。
				`method="get"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s页面缺少筛选栏元素 %q", p.name, want)
				}
			}
		})
	}
}

// TestProductAttributeVariationFilter 属性页的「参与变体」维度真的下推到查询。
//
// 单独一条是因为它多一维下拉：取值必须与 service 的 ListAttributeReq.Variation 同一套
// （"" / "1" / "0"）—— 写错时页面不报错，只表现为「筛了等于没筛」。
func TestProductAttributeVariationFilter(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	ctx := t.Context()
	if _, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "变体颜色", Key: "vcolor", IsVariation: boolPtr(true),
	}); err != nil {
		t.Fatalf("创建参与变体的属性组失败: %v", err)
	}
	if _, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "普通规格", Key: "plain", IsVariation: boolPtr(false),
	}); err != nil {
		t.Fatalf("创建不参与变体的属性组失败: %v", err)
	}

	// 控件回显：选中的必须是当前档位，而不是永远停在第一项。
	all := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID)
	for _, want := range []string{
		`class="filter-actions"`,
		`name="variation"`,
		`<option value="1"`,
		`<option value="0"`,
		"参与变体",
		"不参与变体",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("属性页筛选栏缺少 %q", want)
		}
	}
	noVar := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&variation=0")
	if !strings.Contains(noVar, `<option value="0" selected>`) {
		t.Fatal("variation=0 时下拉未回显为选中态")
	}
	if strings.Contains(noVar, "变体颜色") || !strings.Contains(noVar, "普通规格") {
		t.Fatal("variation=0 应只列出「不参与变体」的属性组")
	}
	withVar := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&variation=1")
	if !strings.Contains(withVar, "变体颜色") || strings.Contains(withVar, "普通规格") {
		t.Fatal("variation=1 应只列出「参与变体」的属性组")
	}
	// 关键词维度：筛一个不存在的词 → 空态但表头仍在（③ 的同一条约定，属性页一并钉住）。
	none := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&keyword=zzznomatch")
	if !strings.Contains(none, "<thead") || !strings.Contains(none, `class="data-table"`) {
		t.Fatal("属性页筛选无结果时表头消失")
	}
	if !strings.Contains(none, "没有匹配的属性组") {
		t.Fatal("属性页筛选无结果时应给「筛出来是空的」文案")
	}
}

// TestProductListPaginationKeepsFilter ② 超过一页时分页条出现，翻页链接保留筛选条件。
func TestProductListPaginationKeepsFilter(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	ctx := t.Context()
	// 25 条同名前缀的品牌 → 每页 20 条，正好两页（第 2 页 5 条）。
	const total = 25
	for i := 1; i <= total; i++ {
		if _, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{
			ProjectID: f.projectID,
			Name:      fmt.Sprintf("pg-brand-%02d", i),
		}); err != nil {
			t.Fatalf("创建品牌失败: %v", err)
		}
	}

	first := getWholePage(t, engine, "/admin/product-brands?keyword=pg-brand")
	if got := strings.Count(first, `name="ids"`); got != 20 {
		t.Fatalf("第 1 页应有 20 行，实际 %d 行", got)
	}
	if !strings.Contains(first, `class="pagination"`) {
		t.Fatal("数据超过一页时未渲染分页条")
	}
	if !strings.Contains(first, fmt.Sprintf("共 %d 条", total)) {
		t.Fatalf("分页条未给出总条数（应为「共 %d 条」）", total)
	}
	nav := paginationSection(first)
	if nav == "" {
		t.Fatal("未取到分页条 HTML 段")
	}
	if !strings.Contains(nav, "page=2") {
		t.Fatal("分页条里没有第 2 页链接")
	}
	if !strings.Contains(nav, "keyword=pg-brand") {
		t.Fatal("翻页链接丢了筛选条件（用户会以为筛选失效）")
	}

	second := getWholePage(t, engine, "/admin/product-brands?keyword=pg-brand&page=2")
	if got := strings.Count(second, `name="ids"`); got != total-20 {
		t.Fatalf("第 2 页应有 %d 行，实际 %d 行", total-20, got)
	}
	if got := strings.Count(second, `name="ids"`); got == 20 {
		t.Fatal("第 2 页与第 1 页行数相同 —— 页码没有生效")
	}

	// 页码越界收敛到最后一页：不收敛会出现「表格为空、分页条显示第 999 页」。
	overflow := getWholePage(t, engine, "/admin/product-brands?keyword=pg-brand&page=999")
	if got := strings.Count(overflow, `name="ids"`); got != total-20 {
		t.Fatalf("page=999 应收敛到最后一页（%d 行），实际 %d 行", total-20, got)
	}

	// 属性页同一口径：21 个属性组分两页，第 2 页必须真的有第 21 个 ——
	// 这正是「Size:200 静默截断」修好之后才可能出现的形态。
	attrEngine, af := newAttrPageEngine(t)
	if attrEngine == nil {
		return
	}
	for i := 1; i <= 21; i++ {
		if _, err := af.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
			ProjectID: af.projectID,
			Name:      fmt.Sprintf("pg-attr-%02d", i),
			Key:       fmt.Sprintf("pg-attr-%02d", i),
		}); err != nil {
			t.Fatalf("创建属性组失败: %v", err)
		}
	}
	attrFirst := getWholePage(t, attrEngine, "/admin/product-attributes?project="+af.projectID)
	if got := strings.Count(attrFirst, `name="ids"`); got != 20 {
		t.Fatalf("属性页第 1 页应有 20 行，实际 %d 行", got)
	}
	if !strings.Contains(attrFirst, "共 21 条") {
		t.Fatal("属性页分页条未给出总条数（应为「共 21 条」）")
	}
	attrSecond := getWholePage(t, attrEngine, "/admin/product-attributes?project="+af.projectID+"&page=2")
	if got := strings.Count(attrSecond, `name="ids"`); got != 1 {
		t.Fatalf("属性页第 2 页应有 1 行（第 21 个属性组），实际 %d 行", got)
	}
}

// TestProductListFilterI18nSeedIsIdempotent 迁移 411 的种子 SQL 能执行、幂等、门槛可命中。
//
// 为什么单独钉一条：迁移「注册了但幂等条件写错 ⇒ 每次启动重跑」或「写错语法但没人跑到」
// 是本项目踩过的坑（178 的 `?` 被迁移器换成表名、104 与 122 的「删了又被 seed 插回」），
// 而页面上看不出来 —— 没有 i18n 组件时模板走 fallback 文案，与词条值同形。
//
// 测试库只跑结构迁移、不跑 RunSeeds（seeds 属于启动路径，见 public/test/support 的组件配置），
// 所以这里从 AllSeeds() 取出本批那条自己执行两遍：一遍验证 SQL 能过、一遍验证幂等；
// 最后再验证 ConditionSQL 在词条落库后返回 > 0（否则那条迁移每次启动都会重跑）。
func TestProductListFilterI18nSeedIsIdempotent(t *testing.T) {
	_, f := newTaxonomyPageEngine(t)
	if f == nil {
		return
	}
	keys := []string{
		"admin.common.filter.submit", "admin.common.filter.reset",
		"admin.common.filter.optionAll", "admin.common.filter.emptyHint",
		"admin.product_attributes.filter.phKeyword", "admin.product_attributes.list.emptyFilteredTitle",
		"admin.product_categories.filter.phKeyword", "admin.product_categories.empty.filteredTitle",
		"admin.product_brands.filter.phKeyword", "admin.product_brands.empty.filteredTitle",
		"admin.product_tags.filter.phKeyword", "admin.product_tags.list.emptyFilteredTitle",
	}
	seeds := migrations.AllSeeds()
	var target *migrations.Seed
	for i := range seeds {
		if seeds[i].Version == "411-i18n-product-list-filter" {
			target = &seeds[i]
			break
		}
	}
	if target == nil {
		t.Fatal("迁移 411 未注册（register_product_list_filter_i18n.go 的 init 没跑到？）")
	}

	rows := func() int64 {
		var n int64
		if err := f.db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ?`, keys).Scan(&n).Error; err != nil {
			t.Fatalf("查询 sys_i18n 失败: %v", err)
		}
		return n
	}
	want := int64(len(keys) * 2) // 12 个 key × 中英双语
	if err := f.db.Exec(target.SQL).Error; err != nil {
		t.Fatalf("执行 411 的种子 SQL 失败: %v", err)
	}
	if got := rows(); got != want {
		t.Fatalf("首次执行后应有 %d 行，实际 %d", want, got)
	}
	if err := f.db.Exec(target.SQL).Error; err != nil {
		t.Fatalf("重复执行 411 的种子 SQL 失败: %v", err)
	}
	if got := rows(); got != want {
		t.Fatalf("重复执行后行数应不变（%d），实际 %d —— ON CONFLICT DO NOTHING 没生效", want, got)
	}

	var hit int64
	if err := f.db.Raw(target.ConditionSQL).Scan(&hit).Error; err != nil {
		t.Fatalf("执行 ConditionSQL 失败: %v", err)
	}
	if hit == 0 {
		t.Fatal("词条已落库但 ConditionSQL 判定为 0 —— 这条迁移会每次启动都重跑")
	}
}

// TestCategoryKeywordFilterKeepsParentOptions 搜索把命中渲染成树（根 + 路径），父级下拉保留全部父项。
//
// 分类页的父级下拉是「编辑 / 新建抽屉」用的：如果只把筛选后（或当前这一页）的树喂给它，
// 用户筛出一个子分类后打开编辑，它的父级会从选项里消失 —— 保存就会把层级静默拍平
// （看起来只是「下拉里没有那一项」）。所以下拉恒取工程内全部分类。
func TestCategoryKeywordFilterKeepsParentOptions(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	ctx := t.Context()
	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "户外服装", Slug: "outdoor",
	})
	if err != nil {
		t.Fatalf("创建父分类失败: %v", err)
	}
	child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "冲锋衣", Slug: "jackets", ParentID: root.ID,
	})
	if err != nil {
		t.Fatalf("创建子分类失败: %v", err)
	}

	body := getWholePage(t, engine, "/admin/product-categories?project="+f.projectID+"&keyword="+url.QueryEscape("冲锋衣"))
	if !strings.Contains(body, "冲锋衣") {
		t.Fatal("关键词命中的子分类没有出现在表格里")
	}
	// 命中项连着它的上级路径一起渲染成树：两行（根 + 命中），根在前、命中缩进成子级。
	if got := strings.Count(body, `name="ids"`); got != 2 {
		t.Fatalf("按「冲锋衣」筛选后应展示命中与祖先共 2 行，实际 %d 行", got)
	}
	if strings.Contains(body, "共 2 条") {
		t.Fatal("分页总数按根分类计，不该把命中条数当页数")
	}
	rootPos := strings.Index(body, `data-category-id="`+root.ID+`"`)
	childPos := strings.Index(body, `data-category-id="`+child.ID+`"`)
	if rootPos < 0 || childPos < 0 || rootPos > childPos {
		t.Fatal("搜索祖先应排在命中子级之前")
	}
	if !strings.Contains(body, `<span class="tree-elbow"`) {
		t.Fatal("命中行没有渲染成子级：缺树枝前缀")
	}
	if !strings.Contains(body, `value="`+root.ID+`"`) {
		t.Fatal("筛选后父级下拉缺了父分类 —— 编辑抽屉里改父级只能改到「顶级」，层级会被静默拍平")
	}
}

// TestProductListEmptyStateKeepsTableHead ③ 空态两档：筛出来是空的 / 工程里本来就没有。
//
// 两档都必须保留 <table>/<thead>：表头是「这个列表有哪些列」的唯一说明，
// 筛出 0 行就把它藏掉，用户只剩一句话、无从判断自己是不是筛错了列。
func TestProductListEmptyStateKeepsTableHead(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	// 先放一条数据，确保页面走的是「有品牌」的正常分支，再做筛选。
	if _, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野品牌",
	}); err != nil {
		t.Fatalf("创建品牌失败: %v", err)
	}

	filtered := getWholePage(t, engine, "/admin/product-brands?keyword=zzznomatch")
	for _, want := range []string{
		`class="data-table"`, "<thead", `class="empty-state"`,
		// colspan = 勾选列 + 品牌 / URL 段 / 排序 / 更新时间 + 操作（SEO 标题列已随
		// 字段合并移除，2026-09-30）。
		`colspan="6"`,
		"没有匹配的品牌",
	} {
		if !strings.Contains(filtered, want) {
			t.Fatalf("筛选无结果的空态缺少 %q", want)
		}
	}
	if strings.Contains(filtered, "还没有品牌") {
		t.Fatal("筛选无结果时不应给「还没有品牌」——分类存在，只是被筛掉了")
	}
	// 筛选无结果的空态要给出路（重置），否则用户只能自己去改地址栏。
	if !strings.Contains(filtered, `href="/admin/product-brands?project=`) {
		t.Fatal("筛选无结果的空态没有「重置」出口")
	}

	// 工程里真的没有品牌：空态换成「还没有品牌」并指向新建抽屉。
	//
	// 这里换一个**新建的隔离库**（fixture 每次新建库）而不是「同库里再建一个空工程」：
	// product_brands 的查询只带 keyword，工程隔离靠 RLS 策略（app.project_id），
	// 而当前测试连接的角色是超级用户、会绕过策略（AGENTS.md 的 DB-009 已知缺口）——
	// 同库里别的工程的品牌会一起被读出来，那个空工程在页面上并不空。
	emptyEngine, _ := newTaxonomyPageEngine(t)
	if emptyEngine == nil {
		return
	}
	empty := getWholePage(t, emptyEngine, "/admin/product-brands")
	for _, want := range []string{`class="data-table"`, "<thead", `class="empty-state"`, `colspan="6"`, "还没有品牌"} {
		if !strings.Contains(empty, want) {
			t.Fatalf("空工程的空态缺少 %q；页面片段=%s", want, clip(empty, 1200))
		}
	}
	if strings.Contains(empty, "没有匹配的品牌") {
		t.Fatal("没有筛选条件时不应给「没有匹配的品牌」")
	}
}
