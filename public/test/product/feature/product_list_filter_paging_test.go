package feature

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

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

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

				`value="zzz"`,
				`placeholder="` + p.placeholder + `"`,
				`class="filter-actions"`,

				`method="get"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s页面缺少筛选栏元素 %q", p.name, want)
				}
			}
		})
	}
}

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

	none := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&keyword=zzznomatch")
	if !strings.Contains(none, "<thead") || !strings.Contains(none, `class="data-table"`) {
		t.Fatal("属性页筛选无结果时表头消失")
	}
	if !strings.Contains(none, "没有匹配的属性组") {
		t.Fatal("属性页筛选无结果时应给「筛出来是空的」文案")
	}
}

func TestProductListPaginationKeepsFilter(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	ctx := t.Context()

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

	overflow := getWholePage(t, engine, "/admin/product-brands?keyword=pg-brand&page=999")
	if got := strings.Count(overflow, `name="ids"`); got != total-20 {
		t.Fatalf("page=999 应收敛到最后一页（%d 行），实际 %d 行", total-20, got)
	}

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

func TestProductListFilterI18nSeedIsIdempotent(t *testing.T) {
	_, f := newTaxonomyPageEngine(t)
	if f == nil {
		return
	}
	keys := []string{
		"uiFltSubmit", "uiFltReset", "uiFltOptAll", "uiFltEmptyHint",
		"attrFltKw", "attrNoMatches",
		"catFltKw", "catNoMatches",
		"brandFltKw", "brandNoMatches",
		"tagFltKw", "tagNoMatches",
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
	want := int64(len(keys) * 2)
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

func TestProductListEmptyStateKeepsTableHead(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}

	if _, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野品牌",
	}); err != nil {
		t.Fatalf("创建品牌失败: %v", err)
	}

	filtered := getWholePage(t, engine, "/admin/product-brands?keyword=zzznomatch")
	for _, want := range []string{
		`class="data-table"`, "<thead", `class="empty-state"`,

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

	if !strings.Contains(filtered, `href="/admin/product-brands?project=`) {
		t.Fatal("筛选无结果的空态没有「重置」出口")
	}

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
