// product_project_scope_transmit_test.go —— 两处「调用方手里有工程 id，却没交给下游取数一跳」
// 的工程作用域判据（P170）。
//
// 同一缺陷形态在多工程部署下有两种后果：
//
//	· 取商品那一跳不带作用域 → product 侧 resolveProjectID 在「显式工程为空且工程数 != 1」时
//	  回 ErrInvalidParam：整页 400（商品详情页 / 捆绑配置页）或评分退化成空态；
//	· **静默降级**（derr 被刻意忽略的那两跳）：属性组读不回来 → 规格文本只剩 optionKey、
//	  来源属性的勾选清单整块变空 —— 页面 200、不报错、日志干净，用户只看到「没有可勾选属性」。
//
// 为什么既有用例照不出：它们都跑在**单工程**夹具上，兜底解析出的那一个恰好就是唯一的那个，
// 「传了」与「没传」行为等价。本文件在 variantListFixture 之上再建第二个工程（与
// product_detail_template_project_scope_test.go 同法），漏传立刻变成可观测的差异。
//
// 断言的是「多工程下必须成功且内容完整」，不是只看状态码：变体预览那条断言规格文本是
// **属性组名 + 值标签**（「颜色 红」）而不是退化后的原始 key（「color red」）——
// 退化时这条断言不会命中。
package feature

import (
	"context"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// scopeTransmitSpec 规格文本的期望值：属性组名 + 值标签。
//
// 与退化形态（「color red」，组名与值都退回原始 key）刻意不同字：只有正常档才会命中它。
const scopeTransmitSpec = "颜色 红"

// scopeTransmitFixture 双工程夹具：variantListFixture（工程 A + 生产迁移 + 真 inventory / product）
// 之上再建第二个工程；本文件的商品一律建在工程 A，页面 / 端点显式带 project=f.projectID。
//
// 显式带工程而不是靠兜底解析：漏传作用域的那一跳**仍然**会失败（工程数 != 1），
// 判据因此不依赖工程清单的顺序。
type scopeTransmitFixture struct {
	*variantListFixture
	otherProject string
}

// newScopeTransmitEngine 装配两个入口（真实 Jet 模板 + 真实 service）。
func newScopeTransmitEngine(t *testing.T) (*gin.Engine, *scopeTransmitFixture) {
	t.Helper()
	base := newVariantListFixture(t)
	if base == nil {
		return nil, nil
	}
	// 变体预览要读可用量（与生产装配同形）。
	base.products.SetAvailabilityPort(base.inventory)
	other, err := base.projects.Create(context.Background(), &projectdto.CreateReq{Name: "第二站点工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if other.ID == base.projectID {
		t.Fatalf("第二个工程 id 与第一个相同：%s", other.ID)
	}
	projects, lerr := base.projects.List(context.Background())
	if lerr != nil {
		t.Fatalf("读工程清单失败: %v", lerr)
	}
	if len(projects) != 2 {
		t.Fatalf("本文件的判据要求多工程夹具，实际 %d 个工程", len(projects))
	}
	f := &scopeTransmitFixture{variantListFixture: base, otherProject: other.ID}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(base.products, base.products, base.projects)
	engine.GET("/admin/products/detail", adminui.PageSlot(producthttp.ProductDetailPageTemplate), handle.ProductDetailPage)
	engine.POST("/admin/products/variant/preview", handle.ProductsVariantPreview)
	return engine, f
}

// scopeTransmitProduct 建一个可用商品（bundleFixture.mkProduct 恒用工程 A，这里显式给属性组）。
func (f *scopeTransmitFixture) scopeTransmitProduct(t *testing.T, name, slug string, attrIDs []string) *productdto.ProductResp {
	t.Helper()
	price := 88.0
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		AttributeIDs: attrIDs, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return res
}

// scopeTransmitAttr 建一个参与变体的属性组：值标签与 key 刻意不同字（「红」/ red），
// 于是「用了标签」与「退化成 key」在规格文本里可区分。
func (f *scopeTransmitFixture) scopeTransmitAttr(t *testing.T) *productdto.AttributeResp {
	t.Helper()
	attr, err := f.products.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
		Values: []productdto.AttributeValueReq{{Label: "红", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	return attr
}

// scopeTransmitValueID 属性组内某个 key 的值 id（勾选表单用的是值 id）。
func scopeTransmitValueID(t *testing.T, attr *productdto.AttributeResp, key string) string {
	t.Helper()
	for _, v := range attr.Values {
		if v.Key == key {
			return v.ID
		}
	}
	t.Fatalf("属性组 %s 里没有值 %s", attr.Key, key)
	return ""
}

// TestProductDetailPageTransmitsProjectScope 第 1 条：商品详情整页取商品那一跳带作用域。
//
// 漏传时的症状：多工程下 resolveProjectID 回 ErrInvalidParam，被读模型的 ErrInvalidParam
// 分支判成 400 —— 整页打不开，而商品就在带上来的那个工程里。
func TestProductDetailPageTransmitsProjectScope(t *testing.T) {
	engine, f := newScopeTransmitEngine(t)
	if engine == nil {
		return
	}
	p := f.scopeTransmitProduct(t, "作用域详情商品", "scope-detail-product", nil)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/detail?project="+f.projectID+"&product="+p.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("多工程站点上打开商品详情页应 200，实际 %d（取商品那一跳漏传作用域 → ErrInvalidParam → 400）：%s",
			rec.Code, oneLine(rec.Body.String()))
	}
	if body := rec.Body.String(); !strings.Contains(body, p.Name) {
		t.Fatalf("详情页应渲染出商品名 %q（不是失败页）：%s", p.Name, oneLine(body))
	}
}

// TestVariantPreviewTransmitsProjectScope 第 4 条：变体预览端点取属性组那一跳带作用域。
//
// 漏传时 derr 被忽略、attrs 退化为空，spec 静默降级成 optionKey（「color red」）——
// 状态码照常 200，只有规格文本看得出来。故断言的是 spec 的字面内容。
func TestVariantPreviewTransmitsProjectScope(t *testing.T) {
	engine, f := newScopeTransmitEngine(t)
	if engine == nil {
		return
	}
	attr := f.scopeTransmitAttr(t)
	valueID := scopeTransmitValueID(t, attr, "red")
	p := f.scopeTransmitProduct(t, "作用域预览商品", "scope-transmit-preview", []string{attr.ID})

	rec := postForm(engine, "/admin/products/variant/preview", url.Values{
		"projectId":       {f.projectID},
		"productId":       {p.ID},
		"warehouseId":     {f.warehouse.ID},
		"attr:" + attr.ID: {valueID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	payload := jsonMap(t, rec.Body.Bytes())
	rows := asList(t, payload["rows"], "rows")
	if len(rows) != 1 {
		t.Fatalf("勾选 1 个值应返回 1 行，实际 %d：%+v", len(rows), payload)
	}
	row := asMap(t, rows[0], "rows[0]")
	if row["spec"] != scopeTransmitSpec {
		t.Fatalf("预览行的规格文本 = %v，want %q（漏传作用域时属性组为空，spec 退化成 optionKey：%q）",
			row["spec"], scopeTransmitSpec, row["optionKey"])
	}
}

// generateCombosFor 在商品上生成指定组合的变体（走「预览 → 保存」这条与后台抽屉同形的路径）。
func (f *scopeTransmitFixture) generateCombosFor(t *testing.T, productID, attributeID, valueID string) {
	t.Helper()
	ctx := context.Background()
	pv, err := f.products.PreviewVariantCombinations(ctx, &productdto.PreviewVariantReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		Selections: []productdto.VariantSelectionReq{{AttributeID: attributeID, ValueIDs: []string{valueID}}},
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	if _, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		OperatorID: "tester", Rows: rows,
	}); err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
}
