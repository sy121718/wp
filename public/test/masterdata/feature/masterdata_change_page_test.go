// masterdata_change_page_test.go — 后台「变更记录」页（issue #19 验收 3 / 4 + 多端适配契约）。
//
// 页面是服务端渲染的只读页，适配手段全部落在模板结构与样式表上，
// 因此这里断言的是「适配所依赖的契约确实存在」，而不是「某个视口看起来还行」：
//
//   - 只用原生控件（input / select / button）—— 鼠标点选、触屏点按、
//     键盘（Tab 进组、回车提交）四条输入路径都由浏览器保证；
//   - 宽表包在可聚焦的横向滚动容器里（tabindex="0"）—— 键盘也能滚动，不依赖鼠标滚轮；
//   - 样式表按断点收口：窄屏改堆叠块（列名用 data-label 回显），输入宽度一律 min(100%, <设计宽度>)；
//   - 页面不引入任何自定义 JS（无 <script>），也没有任何写入口（append-only 的记录不可改写）。
package feature

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

// newMasterDataPageEngine 装配只挂变更记录页的测试引擎（真实 handler + 真实 Jet 渲染）。
func newMasterDataPageEngine(t *testing.T) (*gin.Engine, *mdFixture) {
	t.Helper()
	f := newMDFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := masterdatahttp.NewMasterDataChangePageHandle(f.changes, f.projects)
	engine.GET("/admin/masterdata/changes", handle.MasterDataChangesPage)
	return engine, f
}

// templateRoot 模板根目录（相对本测试包）。
func templateRoot() string {
	return "../../../../internal/templates"
}

// TestMasterDataChangePageByEntity 验收 4：后台可按实体查询变更历史。
func TestMasterDataChangePageByEntity(t *testing.T) {
	engine, f := newMasterDataPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "变更记录 T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	// ① 默认视图：字段级变更列表 + 按实体汇总都在，取值与展示文案都可读。
	rec := httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("变更记录页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"变更记录", "字段级变更", "按实体汇总",
		"SKU 编码", "默认发货仓短码", "上下架状态", // 字段展示名走模块 enums
		"变更记录 T 恤", // 实体展示名快照
		"库存流水",     // 与库存流水的分工写在页面上
		operatorForTest,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}

	// ② 按实体查询：实体类型 + 实体 id 一起给 → 该实体的完整时间线。
	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("按实体查询应 200，实际 %d", rec.Code)
	}
	body = rec.Body.String()
	// 「当前实体」原本是独立卡片，现降级为页头下方的一行上下文（.page-sub）：
	// 同一屏里再放一张只承载这一行信息的卡片，等于白占一张卡的版面。
	for _, want := range []string{"实体 id", v.SKUCode, v.ID} {
		if !strings.Contains(body, want) {
			t.Fatalf("按实体查询的页面缺少 %q", want)
		}
	}
	// 商品实体的记录不该出现在变体的时间线里（按实体是硬筛选，不是关键词匹配）。
	if strings.Contains(body, "变更记录 T 恤") {
		t.Fatalf("按实体查询不应混入其它实体的记录")
	}

	// ③ 筛选维度：按字段筛选只保留该字段的记录。
	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+"&field=sku_code")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SKU 编码") {
		t.Fatalf("按字段筛选应命中 SKU 编码，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "上下架状态") && !strings.Contains(rec.Body.String(), "字段级变更") {
		t.Fatalf("按字段筛选应只保留该字段的记录")
	}

	// ④ 非法筛选值不把页面打挂（错误回显，仍 200）。
	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+"&entityId=not-a-uuid")
	if rec.Code != http.StatusOK || !mentionsMasterDataError(rec.Body.String(), masterdataenums.ErrInvalidParam, "参数不合法") {
		t.Fatalf("非法实体 id 应回显错误且仍 200，实际 %d", rec.Code)
	}
}

// TestMasterDataChangePageAfterUpdate 页面反映「谁在什么时候改了什么」。
func TestMasterDataChangePageAfterUpdate(t *testing.T) {
	engine, f := newMasterDataPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "改价商品", "tee", sz.ID)
	v := f.variant(t, p.ID)
	// 一次改价：谁（carol）在什么时候把售价从 0.00 改成了 77.00 —— 页面逐列展示。
	if _, err := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: v.ID, Price: floatPtr(77), OperatorID: "carol",
	}); err != nil {
		t.Fatalf("改变体失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID+"&action=update")
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"售价", "carol", "77.00", "修改", "0.00"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

// TestMasterDataChangePageMultiDeviceContract 多端 / 多输入适配的结构契约。
func TestMasterDataChangePageMultiDeviceContract(t *testing.T) {
	engine, f := newMasterDataPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "多端适配商品", "tee", sz.ID)
	_ = p

	rec := httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("变更记录页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="stack masterdata-page"`) {
		t.Fatalf("页面缺少多端适配的根类 masterdata-page")
	}
	// 键盘可滚的表格容器（宽表在窄视口下靠它横向滚动，且能 Tab 聚焦后用方向键滚）。
	// 容器类由 pages-table-wrap 迁到公共类 table-wrap（滚动与键盘聚焦都在 .table-wrap 上）。
	// 容器类为 .table-wrap.table-scroll 一**组**：滚动与键盘聚焦在 .table-wrap、
	// 高度封顶与表头吸顶在 .table-scroll，所以断言用正则而不是整串相等匹配。
	if !regexp.MustCompile(`table-wrap[^"]*"[ ]+tabindex="0"`).MatchString(body) {
		t.Fatalf("表格应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}
	// 这一页自己的模板不含任何 <script>：交互全由原生表单（GET 筛选）完成。
	tplRaw, terr := os.ReadFile(templateRoot() + "/admin/masterdata_changes.html")
	if terr != nil {
		t.Fatalf("读变更记录页模板失败: %v", terr)
	}
	tpl := string(tplRaw)
	if strings.Contains(tpl, "<script") {
		t.Fatalf("变更记录页模板不应引入自定义 JS（交互由原生表单完成）")
	}
	// 只读页面：模板里不能出现任何 method="post" 的写入口。
	if strings.Contains(tpl, `method="post"`) {
		t.Fatalf("变更记录是 append-only 的只读页，不该有写表单")
	}
	// 四种输入都落在原生控件上。
	for _, want := range []string{"<input", "<select", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}

	// 样式表契约：断点内的堆叠块（含列名回显）+ 宽度 min(100%, …)。
	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		`.masterdata-page .form-inline input[type="text"]`,
		".masterdata-page-table",
		".masterdata-meta",
		"width: min(100%, 220px)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式表缺少变更记录页规则 %s", want)
		}
	}
	idx := strings.Index(css, "变更记录（admin/masterdata_changes.html，issue #19）")
	if idx < 0 {
		t.Fatalf("样式表缺少变更记录页适配段的说明")
	}
	// 本页适配段从注释头到下一段说明（商品属性管理）为止，整段一起核对。
	block := css[idx:]
	if end := strings.Index(block, "/* ===== 商品属性管理"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, "max-width: 720px") ||
		!strings.Contains(block, ".masterdata-page-table thead { display: none; }") ||
		!strings.Contains(block, `content: attr(data-label) " ";`) {
		t.Fatalf("窄屏断点缺少变更记录表的堆叠规则：%s", block)
	}
}
