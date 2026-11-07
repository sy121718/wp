package feature

import (
	pageview "go_wp/pkg/view"
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
	engine.GET("/admin/masterdata/changes",
		pageview.Slot(masterdatahttp.MasterDataChangesTemplate, pageview.RenderPage),
		handle.MasterDataChangesPage)
	return engine, f
}

func templateRoot() string {
	return "../../../../internal/templates"
}

func TestMasterDataChangePageByEntity(t *testing.T) {
	engine, f := newMasterDataPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "变更记录 T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	rec := httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("变更记录页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"变更记录", "字段级变更", "按实体汇总",
		"SKU 编码", "默认发货仓短码", "上下架状态",
		"变更记录 T 恤",
		"库存流水",
		operatorForTest,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}

	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("按实体查询应 200，实际 %d", rec.Code)
	}
	body = rec.Body.String()

	for _, want := range []string{"实体 id", v.SKUCode, v.ID} {
		if !strings.Contains(body, want) {
			t.Fatalf("按实体查询的页面缺少 %q", want)
		}
	}

	if strings.Contains(body, "变更记录 T 恤") {
		t.Fatalf("按实体查询不应混入其它实体的记录")
	}

	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+"&field=sku_code")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SKU 编码") {
		t.Fatalf("按字段筛选应命中 SKU 编码，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "上下架状态") && !strings.Contains(rec.Body.String(), "字段级变更") {
		t.Fatalf("按字段筛选应只保留该字段的记录")
	}

	rec = httptestGet(engine, "/admin/masterdata/changes?project="+f.projectID+"&entityId=not-a-uuid")
	if rec.Code != http.StatusOK {
		t.Fatalf("非法实体 id 不应把页面打挂（仍 200），实际 %d", rec.Code)
	}
	pageBody := rec.Body.String()
	if !strings.Contains(pageBody, "参数不合法") {
		t.Fatalf("非法实体 id 是客户端输入问题，应回显可行动的「参数不合法」，实际未出现")
	}
	if strings.Contains(pageBody, masterdataenums.ErrInvalidParam) {
		t.Fatalf("页面不应出现 enums 裸 key %q —— 应经 masterDataErrText 取当前语言译文", masterdataenums.ErrInvalidParam)
	}
}

func TestMasterDataChangePageAfterUpdate(t *testing.T) {
	engine, f := newMasterDataPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "改价商品", "tee", sz.ID)
	v := f.variant(t, p.ID)

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

	if !regexp.MustCompile(`table-wrap[^"]*"[ ]+tabindex="0"`).MatchString(body) {
		t.Fatalf("表格应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}

	tplRaw, terr := os.ReadFile(templateRoot() + "/admin/masterdata/masterdata_changes.html")
	if terr != nil {
		t.Fatalf("读变更记录页模板失败: %v", terr)
	}
	tpl := string(tplRaw)
	if strings.Contains(tpl, "<script") {
		t.Fatalf("变更记录页模板不应引入自定义 JS（交互由原生表单完成）")
	}

	if strings.Contains(tpl, `method="post"`) {
		t.Fatalf("变更记录是 append-only 的只读页，不该有写表单")
	}

	for _, want := range []string{"<input", "<select", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}

	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		".filter-field .form-input",
		".masterdata-page-table",
		".masterdata-meta",
		"width: 200px; min-width: 0; max-width: 100%;",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式表缺少变更记录页规则 %s", want)
		}
	}
	idx := strings.Index(css, "变更记录（admin/masterdata_changes.html，issue #19）")
	if idx < 0 {
		t.Fatalf("样式表缺少变更记录页适配段的说明")
	}

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
